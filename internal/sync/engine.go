// Package sync provides IMAP synchronization functionality
package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"regexp"
	"sort"
	"strings"
	gosync "sync"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomessage "github.com/emersion/go-message"
	msgcharset "github.com/emersion/go-message/charset"
	"github.com/hkdb/aerion/internal/email"
	"github.com/hkdb/aerion/internal/folder"
	imapPkg "github.com/hkdb/aerion/internal/imap"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/pgp"
	"github.com/hkdb/aerion/internal/smime"
	"github.com/rs/zerolog"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/htmlindex"
)

func init() {
	// Don't let go-message decode charset - return raw bytes and we'll decode ourselves
	// This gives us full control over charset detection and handling of mislabeled encodings
	gomessage.CharsetReader = func(charsetName string, r io.Reader) (io.Reader, error) {
		// Just return the original reader - we'll handle charset conversion in decodeCharset()
		return r, nil
	}
}

// Batch sizes for incremental sync
const (
	headerBatchSize = 50 // Messages per batch for header fetch
)

// Body fetch batch limits (hybrid byte + count based, like Geary)
const (
	bodyBatchMaxBytes    = 512 * 1024 // 512KB max per batch (memory safety)
	bodyBatchMaxMessages = 50         // Never more than 50 messages per batch
	bodyBatchMinMessages = 1          // At least 1 message per batch (for oversized emails)
	bodyBatchQueryLimit  = 200        // Query more candidates to allow byte-based batching
)

// Size limits for reading to prevent memory exhaustion
const (
	maxPartSize          = 10 * 1024 * 1024 // 10MB max for a single MIME part
	maxMessageSize       = 50 * 1024 * 1024 // 50MB max for entire raw message
	maxInlineContentSize = 5 * 1024 * 1024  // 5MB max for inline image content (stored in DB)
)

// ParsedBody holds the result of parsing a message body, including attachments
type ParsedBody struct {
	BodyText       string
	BodyHTML       string
	HasAttachments bool
	Attachments    []*message.Attachment  // Extracted attachment metadata (content only for inline)
	SMIMEResult    *smime.SignatureResult // S/MIME verification result (nil if not S/MIME)
	SMIMERawBody   []byte                // Raw S/MIME body for on-view processing
	SMIMEEncrypted bool                  // Whether the message is encrypted
	PGPRawBody     []byte                // Raw PGP body for on-view processing
	PGPEncrypted   bool                  // Whether the message is PGP encrypted
}

// Retry limits for error recovery
const (
	maxMessageRetries    = 3 // Max retries per message before giving up
	maxConnectionRetries = 3 // Max connection recovery attempts before aborting
)

// SyncProgress holds progress information for sync operations
type SyncProgress struct {
	AccountID string `json:"accountId"`
	FolderID  string `json:"folderId"`
	Fetched   int    `json:"fetched"`
	Total     int    `json:"total"`
	Phase     string `json:"phase"` // "headers" or "bodies"
}

// ProgressCallback is called with sync progress updates
type ProgressCallback func(progress SyncProgress)

// Engine handles synchronization between IMAP server and local storage
type Engine struct {
	pool             *imapPkg.Pool
	folderStore      *folder.Store
	messageStore     *message.Store
	attachmentStore  *message.AttachmentStore
	attachExtractor  *email.AttachmentExtractor
	sanitizer        *email.Sanitizer
	log              zerolog.Logger
	progressCallback ProgressCallback
	smimeVerifier    *smime.Verifier
	pgpVerifier      *pgp.Verifier
}

// NewEngine creates a new sync engine
func NewEngine(pool *imapPkg.Pool, folderStore *folder.Store, messageStore *message.Store, attachmentStore *message.AttachmentStore) *Engine {
	return &Engine{
		pool:            pool,
		folderStore:     folderStore,
		messageStore:    messageStore,
		attachmentStore: attachmentStore,
		attachExtractor: email.NewAttachmentExtractor(),
		sanitizer:       email.NewSanitizer(),
		log:             logging.WithComponent("sync"),
	}
}

// SetProgressCallback sets the callback function for progress updates
func (e *Engine) SetProgressCallback(callback ProgressCallback) {
	e.progressCallback = callback
}

// SetSMIMEVerifier sets the S/MIME verifier for signature verification during body parsing
func (e *Engine) SetSMIMEVerifier(verifier *smime.Verifier) {
	e.smimeVerifier = verifier
}

// SetPGPVerifier sets the PGP verifier for signature verification during body parsing
func (e *Engine) SetPGPVerifier(verifier *pgp.Verifier) {
	e.pgpVerifier = verifier
}

// ParseRawBody parses raw message bytes into body text/HTML.
// This is a convenience wrapper around ParseDecryptedBody for callers that only need text.
func (e *Engine) ParseRawBody(raw []byte) (bodyHTML, bodyText string) {
	parsed := e.ParseDecryptedBody(raw, "")
	return parsed.BodyHTML, parsed.BodyText
}

// ParseDecryptedBody parses raw message bytes (e.g. from a decrypted S/MIME or PGP envelope)
// and returns the full ParsedBody including attachments.
// This is used by the app layer for on-view processing of encrypted messages.
func (e *Engine) ParseDecryptedBody(raw []byte, messageID string) *ParsedBody {
	parsed := e.parseMessageBodyInternal(raw, messageID)

	if parsed.BodyHTML != "" && e.sanitizer != nil {
		parsed.BodyHTML = e.sanitizer.Sanitize(parsed.BodyHTML)
	}

	return parsed
}

// emitProgress sends progress updates if a callback is set
func (e *Engine) emitProgress(accountID, folderID string, fetched, total int, phase string) {
	if e.progressCallback != nil {
		e.progressCallback(SyncProgress{
			AccountID: accountID,
			FolderID:  folderID,
			Fetched:   fetched,
			Total:     total,
			Phase:     phase,
		})
	}
}

// folderStatusResult holds the result of a parallel STATUS fetch
type folderStatusResult struct {
	mailbox *imapPkg.Mailbox
	status  *imapPkg.Mailbox // STATUS returns same type as LIST but with counts populated
	err     error
}

// Concurrency limit for parallel STATUS fetches
const folderStatusWorkers = 5

// SyncFolders synchronizes the folder list for an account
func (e *Engine) SyncFolders(ctx context.Context, accountID string) error {
	e.log.Debug().Str("account", accountID).Msg("Syncing folders")

	// Get a connection from the pool for LIST
	conn, err := e.pool.GetConnection(ctx, accountID)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer e.pool.Release(conn)

	// List mailboxes from server
	mailboxes, err := conn.Client().ListMailboxes()
	if err != nil {
		return fmt.Errorf("failed to list mailboxes: %w", err)
	}

	totalFolders := len(mailboxes)
	if totalFolders == 0 {
		e.log.Info().Str("account", accountID).Msg("No folders found")
		return nil
	}

	// Emit initial "folders" phase progress
	e.emitProgress(accountID, "", 0, totalFolders, "folders")

	// Get existing local folders
	localFolders, err := e.folderStore.List(accountID)
	if err != nil {
		return fmt.Errorf("failed to list local folders: %w", err)
	}

	// Build a map of local folders by path
	localByPath := make(map[string]*folder.Folder)
	for _, f := range localFolders {
		localByPath[f.Path] = f
	}

	// Fetch STATUS for all folders in parallel
	results := e.fetchFolderStatusParallel(ctx, accountID, mailboxes)

	// Track which paths we've seen and process results
	seenPaths := make(map[string]bool)
	processed := 0

	for _, result := range results {
		mb := result.mailbox
		seenPaths[mb.Name] = true
		processed++

		// Emit progress
		e.emitProgress(accountID, "", processed, totalFolders, "folders")

		// Convert IMAP folder type to our folder type
		folderType := convertFolderType(mb.Type)

		// Skip folders where STATUS failed entirely
		if result.err != nil && result.status == nil {
			e.log.Debug().Err(result.err).Str("mailbox", mb.Name).Msg("Failed to get mailbox status, skipping folder")
			continue
		}

		status := result.status

		// Check if folder exists locally
		if existing, ok := localByPath[mb.Name]; ok {
			// Update existing folder
			existing.Name = extractFolderName(mb.Name, mb.Delimiter)
			existing.Type = folderType
			if status != nil {
				existing.UIDValidity = status.UIDValidity
				existing.UIDNext = status.UIDNext
				existing.HighestModSeq = status.HighestModSeq
				existing.TotalCount = int(status.Messages)
				existing.UnreadCount = int(status.Unseen)
			}

			if err := e.folderStore.Update(existing); err != nil {
				e.log.Warn().Err(err).Str("path", mb.Name).Msg("Failed to update folder")
			}
		} else {
			// Create new folder
			f := &folder.Folder{
				AccountID: accountID,
				Name:      extractFolderName(mb.Name, mb.Delimiter),
				Path:      mb.Name,
				Type:      folderType,
			}
			if status != nil {
				f.UIDValidity = status.UIDValidity
				f.UIDNext = status.UIDNext
				f.HighestModSeq = status.HighestModSeq
				f.TotalCount = int(status.Messages)
				f.UnreadCount = int(status.Unseen)
			}

			// Handle parent folder
			if mb.Delimiter != "" {
				parts := strings.Split(mb.Name, mb.Delimiter)
				if len(parts) > 1 {
					parentPath := strings.Join(parts[:len(parts)-1], mb.Delimiter)
					if parent, ok := localByPath[parentPath]; ok {
						f.ParentID = parent.ID
					}
				}
			}

			if err := e.folderStore.Create(f); err != nil {
				e.log.Warn().Err(err).Str("path", mb.Name).Msg("Failed to create folder")
			} else {
				// Add to local map so child folders can find their parent
				localByPath[f.Path] = f
			}
		}
	}

	// Delete folders that no longer exist on server
	for path, f := range localByPath {
		if !seenPaths[path] {
			e.log.Debug().Str("path", path).Msg("Deleting removed folder")
			if err := e.folderStore.Delete(f.ID); err != nil {
				e.log.Warn().Err(err).Str("path", path).Msg("Failed to delete folder")
			}
		}
	}

	e.log.Info().Str("account", accountID).Int("folders", len(mailboxes)).Msg("Folder sync complete")

	return nil
}

// fetchFolderStatusParallel fetches STATUS for multiple folders concurrently
func (e *Engine) fetchFolderStatusParallel(ctx context.Context, accountID string, mailboxes []*imapPkg.Mailbox) []folderStatusResult {
	results := make([]folderStatusResult, len(mailboxes))

	// Use a semaphore to limit concurrency
	sem := make(chan struct{}, folderStatusWorkers)
	var wg gosync.WaitGroup

	for i, mb := range mailboxes {
		wg.Add(1)
		go func(idx int, mailbox *imapPkg.Mailbox) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = folderStatusResult{mailbox: mailbox, err: ctx.Err()}
				return
			}

			// Get a connection for this STATUS request
			conn, err := e.pool.GetConnection(ctx, accountID)
			if err != nil {
				results[idx] = folderStatusResult{mailbox: mailbox, err: err}
				return
			}
			defer e.pool.Release(conn)

			// Fetch STATUS
			status, err := conn.Client().GetMailboxStatus(ctx, mailbox.Name)
			results[idx] = folderStatusResult{
				mailbox: mailbox,
				status:  status,
				err:     err,
			}
		}(i, mb)
	}

	wg.Wait()
	return results
}

// SyncMessages synchronizes messages for a folder with incremental sync support.
// syncPeriodDays determines how far back to sync (0 = all messages).
// Messages are fetched in two phases: headers first (fast), then bodies (background).
func (e *Engine) SyncMessages(ctx context.Context, accountID, folderID string, syncPeriodDays int) error {
	// Check context at start
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Get folder from store
	f, err := e.folderStore.Get(folderID)
	if err != nil {
		return fmt.Errorf("failed to get folder: %w", err)
	}
	if f == nil {
		return fmt.Errorf("folder not found: %s", folderID)
	}

	e.log.Debug().
		Str("account", accountID).
		Str("folder", f.Path).
		Int("syncPeriodDays", syncPeriodDays).
		Msg("Syncing messages (incremental)")

	// Get a connection from the pool
	conn, err := e.pool.GetConnection(ctx, accountID)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	// Use closure so defer releases the current conn (which may be reassigned during
	// connection recovery in the header batch loop). A bare defer Release(conn) would
	// capture the original pointer and leak the replacement connection.
	defer func() { e.pool.Release(conn) }()

	// Select the mailbox
	mailbox, err := conn.Client().SelectMailbox(ctx, f.Path)
	if err != nil {
		return fmt.Errorf("failed to select mailbox: %w", err)
	}

	// Get mailbox status for accurate unseen count (SELECT doesn't return this)
	mailboxStatus, err := conn.Client().GetMailboxStatus(ctx, f.Path)
	if err != nil {
		e.log.Warn().Err(err).Str("folder", f.Path).Msg("Failed to get mailbox status for unseen count")
		// Continue with sync, will fall back to local count
	}

	// Check for UIDValidity change (mailbox recreated)
	if f.UIDValidity != 0 && f.UIDValidity != mailbox.UIDValidity {
		e.log.Warn().
			Str("folder", f.Path).
			Uint32("old", f.UIDValidity).
			Uint32("new", mailbox.UIDValidity).
			Msg("UIDValidity changed, full resync required")

		// Delete all local messages and resync
		if err := e.messageStore.DeleteByFolder(folderID); err != nil {
			return fmt.Errorf("failed to delete messages: %w", err)
		}
		f.UIDValidity = mailbox.UIDValidity
	}

	// Calculate sync date cutoff
	var sinceDate time.Time
	if syncPeriodDays > 0 {
		sinceDate = time.Now().AddDate(0, 0, -syncPeriodDays)
		e.log.Debug().
			Time("sinceDate", sinceDate).
			Int("syncPeriodDays", syncPeriodDays).
			Msg("Using date-based sync filter")

		// Delete local messages older than sync period
		deleted, err := e.messageStore.DeleteOlderThan(accountID, sinceDate)
		if err != nil {
			e.log.Warn().Err(err).Msg("Failed to delete old messages")
		} else if deleted > 0 {
			e.log.Info().Int("deleted", deleted).Msg("Deleted messages older than sync period")
		}
	}

	// Get local message UIDs
	localUIDs, err := e.messageStore.GetAllUIDs(folderID)
	if err != nil {
		return fmt.Errorf("failed to get local UIDs: %w", err)
	}
	localUIDSet := make(map[uint32]bool)
	for _, uid := range localUIDs {
		localUIDSet[uid] = true
	}

	// Check context before fetching UIDs
	if ctx.Err() != nil {
		e.log.Debug().Msg("Header sync cancelled before fetching UIDs")
		return ctx.Err()
	}

	// Emit "messages" phase - fetching message list from server (UID SEARCH)
	e.emitProgress(accountID, folderID, 0, 0, "messages")

	// Fetch UIDs from server (filtered by date if syncPeriodDays > 0)
	var remoteUIDs []uint32
	if syncPeriodDays > 0 {
		remoteUIDs, err = e.fetchUIDsSince(ctx, conn.Client().RawClient(), sinceDate)
	} else {
		remoteUIDs, err = e.fetchAllUIDs(ctx, conn.Client().RawClient())
	}
	if err != nil {
		e.log.Error().Err(err).Str("folder", f.Path).Msg("Failed to fetch UIDs from server - aborting sync to prevent data loss")
		return fmt.Errorf("failed to fetch UIDs: %w", err)
	}

	e.log.Debug().
		Str("folder", f.Path).
		Int("localCount", len(localUIDs)).
		Int("remoteCount", len(remoteUIDs)).
		Msg("UID comparison")

	// SAFEGUARD: If remote returns empty but we have local messages, something is wrong
	// This could be a network issue, server error, or connection problem
	// Do NOT delete local messages in this case (unless we're using date filtering)
	if len(remoteUIDs) == 0 && len(localUIDs) > 0 && syncPeriodDays == 0 {
		e.log.Warn().
			Str("folder", f.Path).
			Int("localCount", len(localUIDs)).
			Msg("Server returned 0 messages but we have local messages - skipping deletion to prevent data loss")
		// Still try to update folder metadata but don't delete anything
		now := time.Now()
		f.LastSync = &now
		if err := e.folderStore.Update(f); err != nil {
			e.log.Warn().Err(err).Msg("Failed to update folder sync state")
		}
		return nil
	}

	remoteUIDSet := make(map[uint32]bool)
	for _, uid := range remoteUIDs {
		remoteUIDSet[uid] = true
	}

	// Find new UIDs (on server but not local)
	var newUIDs []uint32
	for uid := range remoteUIDSet {
		if !localUIDSet[uid] {
			newUIDs = append(newUIDs, uid)
		}
	}

	// Find deleted UIDs (local but not on server within sync period)
	var deletedUIDs []uint32
	for uid := range localUIDSet {
		if !remoteUIDSet[uid] {
			deletedUIDs = append(deletedUIDs, uid)
		}
	}

	// SAFEGUARD: Warn if we're about to delete a large percentage of messages
	// This could indicate a problem with the sync rather than actual deletions
	if len(localUIDs) > 10 && len(deletedUIDs) > len(localUIDs)/2 && syncPeriodDays == 0 {
		e.log.Warn().
			Str("folder", f.Path).
			Int("localCount", len(localUIDs)).
			Int("deletedCount", len(deletedUIDs)).
			Msg("About to delete more than 50% of local messages - this may indicate a sync issue")
	}

	// Delete removed messages
	for _, uid := range deletedUIDs {
		if err := e.messageStore.DeleteByUID(folderID, uid); err != nil {
			e.log.Warn().Err(err).Uint32("uid", uid).Msg("Failed to delete message")
		}
	}

	// Sync flags for existing messages (messages that exist both locally and on server)
	var existingUIDs []uint32
	for uid := range localUIDSet {
		if remoteUIDSet[uid] {
			existingUIDs = append(existingUIDs, uid)
		}
	}

	if len(existingUIDs) > 0 {
		e.log.Debug().Int("count", len(existingUIDs)).Msg("Syncing flags for existing messages")
		if err := e.syncMessageFlags(ctx, conn.Client().RawClient(), folderID, existingUIDs); err != nil {
			e.log.Warn().Err(err).Msg("Failed to sync message flags")
			// Continue with sync even if flag sync fails
		}
	}

	// Fetch new messages with incremental approach (headers first)
	if len(newUIDs) > 0 {
		// Sort UIDs descending (newest first)
		sort.Slice(newUIDs, func(i, j int) bool {
			return newUIDs[i] > newUIDs[j]
		})

		e.log.Info().
			Int("count", len(newUIDs)).
			Msg("Fetching new messages (headers first)")

		// Track connection recovery attempts for header sync
		headerConnectionFailures := 0

		// Fetch headers in batches
		for i := 0; i < len(newUIDs); i += headerBatchSize {
			// Check context at start of each batch
			if ctx.Err() != nil {
				e.log.Debug().Msg("Header sync cancelled")
				return ctx.Err()
			}

			end := i + headerBatchSize
			if end > len(newUIDs) {
				end = len(newUIDs)
			}
			batch := newUIDs[i:end]

			// Emit progress
			e.emitProgress(accountID, folderID, i, len(newUIDs), "headers")

			// Fetch headers for this batch with retry on connection error
			batchRetries := 0
			for {
				err := e.fetchMessageHeaders(ctx, conn.Client().RawClient(), accountID, folderID, batch)
				if err == nil {
					break // Success
				}

				// Check if this was a cancellation
				if ctx.Err() != nil {
					e.log.Debug().Msg("Header sync cancelled during batch fetch")
					return ctx.Err()
				}

				// Check if this is a connection error
				if imapPkg.IsConnectionError(err) {
					headerConnectionFailures++
					batchRetries++

					// Check if we've exhausted connection recovery attempts
					if headerConnectionFailures > maxConnectionRetries {
						e.log.Error().
							Int("connectionFailures", headerConnectionFailures).
							Msg("Header sync aborted - connection recovery failed")
						return fmt.Errorf("header sync connection recovery failed after %d attempts", headerConnectionFailures)
					}

					e.log.Debug().
						Err(err).
						Int("attempt", headerConnectionFailures).
						Int("batchRetry", batchRetries).
						Msg("Connection error during header fetch, attempting recovery")

					// Discard dead connection and get a new one
					e.pool.Discard(conn)

					conn, err = e.pool.GetConnection(ctx, accountID)
					if err != nil {
						return fmt.Errorf("failed to get new connection during header sync: %w", err)
					}

					// Re-select mailbox on new connection
					_, err = conn.Client().SelectMailbox(ctx, f.Path)
					if err != nil {
						e.pool.Release(conn)
						return fmt.Errorf("failed to select mailbox on new connection: %w", err)
					}

					e.log.Debug().Msg("Connection recovered for header sync")
					// Retry this batch with the new connection
					continue
				}

				// Non-connection error - log and continue to next batch
				e.log.Warn().Err(err).Int("batch", i/headerBatchSize).Msg("Failed to fetch header batch")
				break
			}
		}

		// Emit final progress for headers
		e.emitProgress(accountID, folderID, len(newUIDs), len(newUIDs), "headers")
	} else {
		// No new messages - emit 1/1 so frontend shows 100% complete, not 0%
		// (0/0 would result in 0% which looks like it's stuck)
		e.emitProgress(accountID, folderID, 1, 1, "headers")
	}

	// Update sync state
	now := time.Now()
	f.UIDValidity = mailbox.UIDValidity
	f.UIDNext = mailbox.UIDNext
	f.HighestModSeq = mailbox.HighestModSeq
	f.TotalCount = int(mailbox.Messages)
	f.LastSync = &now

	// Use IMAP server's authoritative unread count if available
	if mailboxStatus != nil {
		f.UnreadCount = int(mailboxStatus.Unseen)
	} else {
		// Fall back to counting local messages if status call failed
		unreadCount, err := e.messageStore.CountUnreadByFolder(folderID)
		if err == nil {
			f.UnreadCount = unreadCount
		}
	}

	if err := e.folderStore.Update(f); err != nil {
		e.log.Warn().Err(err).Msg("Failed to update folder sync state")
	}

	e.log.Info().
		Str("folder", f.Path).
		Int("new", len(newUIDs)).
		Int("deleted", len(deletedUIDs)).
		Msg("Message sync complete (headers)")

	return nil
}

// syncMessageFlags fetches and updates flags for existing messages from the IMAP server.
// This ensures local message flags stay in sync with server changes (e.g., webmail).
func (e *Engine) syncMessageFlags(ctx context.Context, client *imapclient.Client, folderID string, uids []uint32) error {
	if len(uids) == 0 {
		return nil
	}

	// Fetch flags in batches to avoid overwhelming the server
	const flagBatchSize = 500
	for i := 0; i < len(uids); i += flagBatchSize {
		// Check for cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}

		end := i + flagBatchSize
		if end > len(uids) {
			end = len(uids)
		}
		batch := uids[i:end]

		// Convert to imap.UIDSet
		uidSet := imap.UIDSet{}
		for _, uid := range batch {
			uidSet.AddNum(imap.UID(uid))
		}

		// Fetch only flags for these UIDs
		fetchOptions := &imap.FetchOptions{
			Flags: true,
		}

		fetchCmd := client.Fetch(uidSet, fetchOptions)

		// Collect all flag updates for batch DB update
		var flagUpdates []message.FlagUpdate

		for {
			msg := fetchCmd.Next()
			if msg == nil {
				break
			}

			// Collect the fetch data
			var fetchedUID uint32
			var isRead, isStarred, isAnswered, isForwarded, isDraft, isDeleted bool

			for {
				item := msg.Next()
				if item == nil {
					break
				}

				switch data := item.(type) {
				case imapclient.FetchItemDataUID:
					fetchedUID = uint32(data.UID)
				case imapclient.FetchItemDataFlags:
					for _, flag := range data.Flags {
						switch flag {
						case imap.FlagSeen:
							isRead = true
						case imap.FlagFlagged:
							isStarred = true
						case imap.FlagAnswered:
							isAnswered = true
						case imap.FlagDraft:
							isDraft = true
						case imap.FlagDeleted:
							isDeleted = true
						case "$Forwarded", "\\Forwarded":
							isForwarded = true
						}
					}
				}
			}

			// Collect flag update for batch processing
			if fetchedUID > 0 {
				flagUpdates = append(flagUpdates, message.FlagUpdate{
					UID:         fetchedUID,
					IsRead:      isRead,
					IsStarred:   isStarred,
					IsAnswered:  isAnswered,
					IsForwarded: isForwarded,
					IsDraft:     isDraft,
					IsDeleted:   isDeleted,
				})
			}
		}

		if err := fetchCmd.Close(); err != nil {
			return fmt.Errorf("failed to fetch flags: %w", err)
		}

		// Batch update all flags in a single transaction
		if len(flagUpdates) > 0 {
			if err := e.messageStore.UpdateFlagsByUIDBatch(folderID, flagUpdates); err != nil {
				e.log.Warn().Err(err).Int("count", len(flagUpdates)).Msg("Failed to batch update message flags")
			}
		}
	}

	e.log.Debug().Int("count", len(uids)).Msg("Synced message flags")
	return nil
}

// fetchUIDsSince fetches UIDs of messages since the given date.
// Uses a goroutine to allow context cancellation since Wait() blocks indefinitely.
func (e *Engine) fetchUIDsSince(ctx context.Context, client *imapclient.Client, since time.Time) ([]uint32, error) {
	e.log.Debug().Time("since", since).Msg("Fetching UIDs since date")

	// UID Search for messages since the given date
	searchCmd := client.UIDSearch(&imap.SearchCriteria{
		Since: since,
	}, nil)

	// Run Wait() in a goroutine to allow context cancellation
	type searchResult struct {
		data *imap.SearchData
		err  error
	}
	resultCh := make(chan searchResult, 1)
	go func() {
		data, err := searchCmd.Wait()
		resultCh <- searchResult{data, err}
	}()

	// Wait for either result or context cancellation
	select {
	case <-ctx.Done():
		e.log.Debug().Msg("UID search (since) cancelled by context")
		return nil, ctx.Err()
	case result := <-resultCh:
		if result.err != nil {
			e.log.Error().Err(result.err).Msg("UID search (since) command failed")
			return nil, fmt.Errorf("UID search failed: %w", result.err)
		}

		var uids []uint32
		for _, uid := range result.data.AllUIDs() {
			uids = append(uids, uint32(uid))
		}

		e.log.Debug().Int("count", len(uids)).Msg("Fetched UIDs since date")
		return uids, nil
	}
}

// fetchMessageHeaders fetches only headers (envelope, flags) for the given UIDs.
// Messages are saved with BodyFetched=false, bodies to be fetched later.
func (e *Engine) fetchMessageHeaders(ctx context.Context, client *imapclient.Client, accountID, folderID string, uids []uint32) error {
	if len(uids) == 0 {
		return nil
	}

	e.log.Debug().Int("count", len(uids)).Msg("Fetching message headers")

	// Convert to imap.UIDSet
	uidSet := imap.UIDSet{}
	for _, uid := range uids {
		uidSet.AddNum(imap.UID(uid))
	}

	// Fetch only envelope, flags, size, internal date, and HEADER (not full body)
	fetchOptions := &imap.FetchOptions{
		Envelope:     true,
		Flags:        true,
		RFC822Size:   true,
		InternalDate: true,
		UID:          true,
		BodySection: []*imap.FetchItemBodySection{
			{
				Specifier: imap.PartSpecifierHeader, // Only headers, not body
				Peek:      true,
			},
		},
	}

	fetchCmd := client.Fetch(uidSet, fetchOptions)

	// Stream messages one at a time instead of blocking on Collect()
	// This allows cancellation between messages and prevents indefinite blocking
	var savedMessages []*message.Message
	fetchedCount := 0

	for {
		// Check for cancellation between messages
		if ctx.Err() != nil {
			fetchCmd.Close()
			e.log.Warn().
				Int("fetched", fetchedCount).
				Int("requested", len(uids)).
				Msg("Header fetch cancelled, saved partial results")
			// Don't return error - we saved what we got
			break
		}

		msg := fetchCmd.Next()
		if msg == nil {
			break
		}

		// Collect all data items for this message
		var fetchedUID imap.UID
		var envelope *imap.Envelope
		var flags []imap.Flag
		var rfc822Size int64
		var headerBytes []byte

		for {
			item := msg.Next()
			if item == nil {
				break
			}

			switch data := item.(type) {
			case imapclient.FetchItemDataUID:
				fetchedUID = data.UID
			case imapclient.FetchItemDataEnvelope:
				envelope = data.Envelope
			case imapclient.FetchItemDataFlags:
				flags = data.Flags
			case imapclient.FetchItemDataRFC822Size:
				rfc822Size = data.Size
			case imapclient.FetchItemDataBodySection:
				// Read header bytes from literal reader
				if data.Literal != nil {
					var err error
					headerBytes, err = io.ReadAll(data.Literal)
					if err != nil {
						e.log.Warn().Err(err).Uint32("uid", uint32(fetchedUID)).Msg("Failed to read header literal")
					}
				}
			}
		}

		if fetchedUID == 0 {
			e.log.Warn().Msg("Received message without UID in header fetch")
			continue
		}

		// Build message from streamed data
		m := &message.Message{
			AccountID:   accountID,
			FolderID:    folderID,
			UID:         uint32(fetchedUID),
			ReceivedAt:  time.Now().UTC(),
			BodyFetched: false, // Headers only, no body yet
			Size:        int(rfc822Size),
		}

		// Parse envelope
		var references []string
		if envelope != nil {
			m.Subject = envelope.Subject
			m.MessageID = envelope.MessageID
			if len(envelope.InReplyTo) > 0 {
				m.InReplyTo = envelope.InReplyTo[0]
			}
			m.Date = envelope.Date.UTC()

			// From
			if len(envelope.From) > 0 {
				m.FromName = envelope.From[0].Name
				m.FromEmail = envelope.From[0].Addr()
			}

			// To
			if len(envelope.To) > 0 {
				m.ToList = addressListToJSON(envelope.To)
			}

			// Cc
			if len(envelope.Cc) > 0 {
				m.CcList = addressListToJSON(envelope.Cc)
			}

			// Reply-To
			if len(envelope.ReplyTo) > 0 {
				m.ReplyTo = envelope.ReplyTo[0].Addr()
			}
		}

		// Extract References and read receipt header from header bytes
		if len(headerBytes) > 0 {
			references = e.extractReferences(headerBytes)
			m.ReadReceiptTo = e.extractDispositionNotificationTo(headerBytes)

			// Check for attachments from Content-Type header (heuristic)
			headerStr := string(headerBytes)
			if strings.Contains(strings.ToLower(headerStr), "multipart/mixed") ||
				strings.Contains(strings.ToLower(headerStr), "application/") {
				m.HasAttachments = true
			}
		}

		// Store references as JSON array
		if len(references) > 0 {
			refsJSON, _ := json.Marshal(references)
			m.References = string(refsJSON)
		}

		// Parse flags
		for _, flag := range flags {
			switch flag {
			case imap.FlagSeen:
				m.IsRead = true
			case imap.FlagFlagged:
				m.IsStarred = true
			case imap.FlagAnswered:
				m.IsAnswered = true
			case imap.FlagDraft:
				m.IsDraft = true
			case imap.FlagDeleted:
				m.IsDeleted = true
			case "$Forwarded", "\\Forwarded":
				m.IsForwarded = true
			}
		}

		// Save to store immediately (don't wait for all messages)
		if err := e.messageStore.Create(m); err != nil {
			e.log.Warn().Err(err).Uint32("uid", m.UID).Msg("Failed to save message header")
			continue
		}
		savedMessages = append(savedMessages, m)
		fetchedCount++
	}

	if err := fetchCmd.Close(); err != nil {
		e.log.Warn().Err(err).
			Int("fetched", fetchedCount).
			Int("requested", len(uids)).
			Msg("Header fetch close error, continuing with saved messages")
		// Don't return error - we saved what we got
	}

	e.log.Debug().
		Int("fetched", fetchedCount).
		Int("requested", len(uids)).
		Msg("Header fetch complete")

	// Compute thread IDs after saving and reconcile related messages
	for _, m := range savedMessages {
		threadID := e.computeThreadID(accountID, m)
		if threadID != "" && threadID != m.ThreadID {
			m.ThreadID = threadID
			if err := e.messageStore.UpdateThreadID(m.ID, threadID); err != nil {
				e.log.Warn().Err(err).Str("messageId", m.ID).Msg("Failed to update thread ID")
			}
		}

		// Reconcile threads: link this message with related messages
		// This handles cases where replies were synced before the original message
		if err := e.messageStore.ReconcileThreadsForNewMessage(accountID, m.ID, m.MessageID, m.ThreadID, m.InReplyTo); err != nil {
			e.log.Warn().Err(err).Str("messageId", m.ID).Msg("Failed to reconcile threads")
		}
	}

	return nil
}

// parseMessageHeaderBuffer parses an IMAP FetchMessageBuffer containing only headers
func (e *Engine) parseMessageHeaderBuffer(accountID, folderID string, buf *imapclient.FetchMessageBuffer) (*message.Message, error) {
	m := &message.Message{
		AccountID:   accountID,
		FolderID:    folderID,
		UID:         uint32(buf.UID),
		ReceivedAt:  time.Now().UTC(),
		BodyFetched: false, // Headers only, no body yet
	}

	// Parse envelope
	envelope := buf.Envelope
	var references []string
	if envelope != nil {
		m.Subject = envelope.Subject
		m.MessageID = envelope.MessageID
		if len(envelope.InReplyTo) > 0 {
			m.InReplyTo = envelope.InReplyTo[0]
		}
		m.Date = envelope.Date.UTC()

		// From
		if len(envelope.From) > 0 {
			m.FromName = envelope.From[0].Name
			m.FromEmail = envelope.From[0].Addr()
		}

		// To
		if len(envelope.To) > 0 {
			m.ToList = addressListToJSON(envelope.To)
		}

		// Cc
		if len(envelope.Cc) > 0 {
			m.CcList = addressListToJSON(envelope.Cc)
		}

		// Reply-To
		if len(envelope.ReplyTo) > 0 {
			m.ReplyTo = envelope.ReplyTo[0].Addr()
		}
	}

	// Extract References and read receipt header from headers
	for _, section := range buf.BodySection {
		if len(section.Bytes) > 0 {
			references = e.extractReferences(section.Bytes)
			m.ReadReceiptTo = e.extractDispositionNotificationTo(section.Bytes)

			// Check for attachments from Content-Type header
			// This is a heuristic - we'll confirm when fetching body
			headerStr := string(section.Bytes)
			if strings.Contains(strings.ToLower(headerStr), "multipart/mixed") ||
				strings.Contains(strings.ToLower(headerStr), "application/") {
				m.HasAttachments = true
			}
			break
		}
	}

	// Store references as JSON array
	if len(references) > 0 {
		refsJSON, _ := json.Marshal(references)
		m.References = string(refsJSON)
	}

	// Parse flags
	for _, flag := range buf.Flags {
		switch flag {
		case imap.FlagSeen:
			m.IsRead = true
		case imap.FlagFlagged:
			m.IsStarred = true
		case imap.FlagAnswered:
			m.IsAnswered = true
		case imap.FlagDraft:
			m.IsDraft = true
		case imap.FlagDeleted:
			m.IsDeleted = true
		case "$Forwarded", "\\Forwarded":
			m.IsForwarded = true
		}
	}

	// Size
	m.Size = int(buf.RFC822Size)

	// No snippet yet - will be generated when body is fetched

	return m, nil
}

// FetchMessageBody fetches the body for a single message on-demand.
// Uses streaming fetch internally to avoid blocking on .Collect().
func (e *Engine) FetchMessageBody(ctx context.Context, accountID, messageID string) (*message.Message, error) {
	// Get message from store to get UID and folder
	uid, folderID, err := e.messageStore.GetMessageUIDAndFolder(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message info: %w", err)
	}

	// Get folder to get path
	f, err := e.folderStore.Get(folderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get folder: %w", err)
	}

	e.log.Debug().
		Str("messageID", messageID).
		Uint32("uid", uid).
		Str("folder", f.Path).
		Msg("Fetching message body on-demand")

	// Get a connection from the pool
	conn, err := e.pool.GetConnection(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer e.pool.Release(conn)

	// Select the mailbox
	_, err = conn.Client().SelectMailbox(ctx, f.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to select mailbox: %w", err)
	}

	// Use fetchMessageBodiesBatch for streaming fetch (avoids .Collect() blocking)
	uidToMessageID := map[uint32]string{uid: messageID}
	results, err := e.fetchMessageBodiesBatch(ctx, conn.Client().RawClient(), uidToMessageID)
	if err != nil {
		return nil, fmt.Errorf("fetch body failed: %w", err)
	}

	result, ok := results[uid]
	if !ok || result == nil {
		return nil, fmt.Errorf("message not found on server")
	}

	// Update message in store
	if err := e.messageStore.UpdateBody(messageID, result.BodyHTML, result.BodyText, result.Snippet); err != nil {
		return nil, fmt.Errorf("failed to update message body: %w", err)
	}

	// Store attachments if present
	if result.HasAttachments && e.attachmentStore != nil {
		for _, att := range result.Attachments {
			if err := e.attachmentStore.Create(att); err != nil {
				e.log.Debug().Err(err).Str("filename", att.Filename).Msg("Failed to save attachment metadata")
			}
		}
	}

	// Return updated message
	return e.messageStore.Get(messageID)
}

// fetchMessageBodyWithConn fetches body using provided connection (no new connection).
// The mailbox must already be selected by the caller.
// This is an internal method used by FetchBodiesInBackground for efficiency.
// Uses fetchMessageBodiesBatch() internally to avoid blocking on .Collect().
func (e *Engine) fetchMessageBodyWithConn(ctx context.Context, client *imapclient.Client, messageID string) error {
	// Check context
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Get message UID from store
	uid, _, err := e.messageStore.GetMessageUIDAndFolder(messageID)
	if err != nil {
		return fmt.Errorf("failed to get message info: %w", err)
	}

	e.log.Debug().
		Str("messageID", messageID).
		Uint32("uid", uid).
		Msg("Fetching message body with existing connection")

	// Use fetchMessageBodiesBatch for streaming fetch (avoids .Collect() blocking)
	uidToMessageID := map[uint32]string{uid: messageID}
	results, err := e.fetchMessageBodiesBatch(ctx, client, uidToMessageID)
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	result, ok := results[uid]
	if !ok || result == nil {
		return fmt.Errorf("message not found on server")
	}

	// Update message in store
	if err := e.messageStore.UpdateBody(messageID, result.BodyHTML, result.BodyText, result.Snippet); err != nil {
		return fmt.Errorf("failed to update message body: %w", err)
	}

	// Store attachments if present
	if result.HasAttachments && e.attachmentStore != nil {
		for _, att := range result.Attachments {
			if err := e.attachmentStore.Create(att); err != nil {
				e.log.Debug().Err(err).Str("filename", att.Filename).Msg("Failed to save attachment metadata")
			}
		}
	}

	return nil
}

// ProcessedBody holds the parsed body content and attachments for a message
type ProcessedBody struct {
	MessageID      string
	BodyHTML       string
	BodyText       string
	Snippet        string
	HasAttachments bool
	Attachments    []*message.Attachment  // Extracted during parsing (no re-parse needed)
	RawBytes       []byte                 // For on-demand attachment content fetch
	SMIMEResult    *smime.SignatureResult  // S/MIME verification result
	SMIMERawBody   []byte                 // Raw S/MIME body for on-view processing
	SMIMEEncrypted bool                   // Whether the message is encrypted
	PGPRawBody     []byte                 // Raw PGP body for on-view processing
	PGPEncrypted   bool                   // Whether the message is PGP encrypted
}

// fetchMessageBodiesBatch fetches bodies for multiple messages in a single IMAP command
// The mailbox must already be selected by the caller.
// Returns a map of UID -> ProcessedBody for successfully fetched messages.
//
// Uses streaming (Next() loop) instead of Collect() to:
// - Avoid indefinite blocking if connection hangs
// - Allow context cancellation between messages
// - Return partial results if connection dies mid-batch
func (e *Engine) fetchMessageBodiesBatch(ctx context.Context, client *imapclient.Client, uidToMessageID map[uint32]string) (map[uint32]*ProcessedBody, error) {
	if len(uidToMessageID) == 0 {
		return make(map[uint32]*ProcessedBody), nil
	}

	// Check context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Build UID set for batch fetch
	uidSet := imap.UIDSet{}
	for uid := range uidToMessageID {
		uidSet.AddNum(imap.UID(uid))
	}

	e.log.Debug().
		Int("count", len(uidToMessageID)).
		Msg("Fetching message bodies in batch")

	fetchOptions := &imap.FetchOptions{
		UID: true,
		BodySection: []*imap.FetchItemBodySection{
			{
				Specifier: imap.PartSpecifierNone, // Full message
				Peek:      true,                   // Don't mark as read
			},
		},
		RFC822Size: true,
	}

	fetchCmd := client.Fetch(uidSet, fetchOptions)
	results := make(map[uint32]*ProcessedBody)

	// Stream messages one at a time instead of blocking on Collect()
	// This allows cancellation between messages and returns partial results on error
	for {
		// Check for cancellation between messages
		if ctx.Err() != nil {
			fetchCmd.Close()
			e.log.Warn().
				Int("fetched", len(results)).
				Int("requested", len(uidToMessageID)).
				Msg("Fetch cancelled, returning partial results")
			return results, ctx.Err()
		}

		msg := fetchCmd.Next()
		if msg == nil {
			break
		}

		// Extract UID and body section from streamed message
		var fetchedUID imap.UID
		var rawBytes []byte
		var gotBodySection bool

		for {
			item := msg.Next()
			if item == nil {
				break
			}

			switch data := item.(type) {
			case imapclient.FetchItemDataUID:
				fetchedUID = data.UID
			case imapclient.FetchItemDataBodySection:
				gotBodySection = true
				// Read body from literal reader with size limit to prevent memory exhaustion
				if data.Literal != nil {
					lr := io.LimitReader(data.Literal, maxMessageSize)
					var err error
					rawBytes, err = io.ReadAll(lr)
					if err != nil {
						e.log.Warn().
							Err(err).
							Uint32("uid", uint32(fetchedUID)).
							Msg("Failed to read body literal, continuing with partial data")
						// Keep whatever we got (may be partial)
					}
					// Log if we hit the size limit
					if int64(len(rawBytes)) == maxMessageSize {
						e.log.Warn().
							Uint32("uid", uint32(fetchedUID)).
							Int64("maxSize", maxMessageSize).
							Msg("Message body truncated at size limit")
					}
				} else {
					e.log.Warn().
						Uint32("uid", uint32(fetchedUID)).
						Msg("Body section has nil Literal reader")
				}
			}
		}

		// Log if we didn't receive a body section at all
		if !gotBodySection && fetchedUID != 0 {
			e.log.Warn().
				Uint32("uid", uint32(fetchedUID)).
				Msg("No body section in IMAP response for message")
		}

		uid := uint32(fetchedUID)
		if uid == 0 {
			e.log.Warn().Msg("Received message without UID in batch response")
			continue
		}

		messageID, ok := uidToMessageID[uid]
		if !ok {
			e.log.Warn().Uint32("uid", uid).Msg("Received unexpected UID in batch response")
			continue
		}

		if len(rawBytes) == 0 {
			e.log.Warn().Uint32("uid", uid).Str("messageID", messageID).Msg("Empty message body in batch")
			continue
		}

		e.log.Debug().
			Uint32("uid", uid).
			Int("bodySize", len(rawBytes)).
			Msg("Processing message body")

		// Parse body content with timeout, extracting attachments in the same pass
		parsed := e.parseMessageBodyFull(rawBytes, messageID, 30*time.Second)

		// Sanitize HTML
		bodyHTML := parsed.BodyHTML
		if bodyHTML != "" {
			bodyHTML = e.sanitizer.Sanitize(bodyHTML)
		}

		// Generate snippet
		var snippet string
		if parsed.BodyText != "" {
			snippet = generateSnippet(parsed.BodyText, 200)
		} else if bodyHTML != "" {
			snippet = generateSnippet(stripHTMLTags(bodyHTML), 200)
		}

		results[uid] = &ProcessedBody{
			MessageID:      messageID,
			BodyHTML:       bodyHTML,
			BodyText:       parsed.BodyText,
			Snippet:        snippet,
			HasAttachments: parsed.HasAttachments,
			Attachments:    parsed.Attachments,
			RawBytes:       rawBytes,
			SMIMEResult:    parsed.SMIMEResult,
			SMIMERawBody:   parsed.SMIMERawBody,
			SMIMEEncrypted: parsed.SMIMEEncrypted,
			PGPRawBody:     parsed.PGPRawBody,
			PGPEncrypted:   parsed.PGPEncrypted,
		}
	}

	if err := fetchCmd.Close(); err != nil {
		e.log.Warn().Err(err).
			Int("fetched", len(results)).
			Int("requested", len(uidToMessageID)).
			Msg("Fetch close error, returning partial results")
		// Return what we have, don't fail completely
		// Partial content is better than no content
	}

	e.log.Debug().
		Int("fetched", len(results)).
		Int("requested", len(uidToMessageID)).
		Msg("Batch fetch complete")

	return results, nil
}

// FetchBodiesInBackground fetches bodies for messages that don't have them yet.
// This is called after headers sync to fetch bodies in the background.
// syncPeriodDays limits body fetching to messages within the sync period (0 = all messages).
//
// OPTIMIZED: Uses batch IMAP FETCH to fetch multiple message bodies in a single command,
// reducing network round-trips significantly. Uses hybrid byte+count batching (like Geary)
// for memory safety and efficiency:
//   - Max 512KB per batch (memory bounded)
//   - Max 50 messages per batch (even if small)
//   - Min 1 message per batch (handles oversized emails)
//
// Pipeline design for maximum throughput:
//  1. Wait for previous batch's goroutine (if any)
//  2. Apply DB updates from previous batch
//  3. Query candidates and build byte-aware batch
//  4. Fetch bodies via IMAP
//  5. Launch goroutine to parse/sanitize (DB update happens in step 2 of next iteration)
//  6. Repeat
//
// This allows IMAP fetch (network-bound) to run in parallel with parsing (CPU-bound).
// DB updates are synchronous relative to the next DB query to prevent race conditions.
//
// Uses a single IMAP connection for efficiency (reuses connection for all body fetches).
// Includes error recovery: on connection errors, discards dead connection and gets a new one.
// Returns error only if connection recovery fails - individual message failures are logged and skipped.
func (e *Engine) FetchBodiesInBackground(ctx context.Context, accountID, folderID string, syncPeriodDays int) error {
	// Check context at start
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Get folder to get path
	f, err := e.folderStore.Get(folderID)
	if err != nil {
		return fmt.Errorf("failed to get folder: %w", err)
	}

	// Calculate sync date cutoff
	var sinceDate time.Time
	if syncPeriodDays > 0 {
		sinceDate = time.Now().AddDate(0, 0, -syncPeriodDays)
	}

	e.log.Debug().
		Str("account", accountID).
		Str("folder", f.Path).
		Int("syncPeriodDays", syncPeriodDays).
		Msg("Fetching message bodies in background (hybrid batch mode)")

	// Get a SINGLE connection from the pool - reused for all body fetches
	conn, err := e.pool.GetConnection(ctx, accountID)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	// Note: We manage connection lifecycle manually due to recovery logic
	// Don't use defer e.pool.Release(conn) - we handle it explicitly

	// Select the mailbox ONCE
	_, err = conn.Client().SelectMailbox(ctx, f.Path)
	if err != nil {
		e.pool.Release(conn)
		return fmt.Errorf("failed to select mailbox: %w", err)
	}

	// Get total count of messages without body (respecting sync period)
	totalWithoutBody, err := e.messageStore.CountMessagesWithoutBody(folderID, sinceDate)
	if err != nil {
		e.pool.Release(conn)
		return fmt.Errorf("failed to count messages without body: %w", err)
	}

	if totalWithoutBody == 0 {
		e.log.Debug().Msg("All messages have bodies, nothing to fetch")
		// Emit 1/1 so frontend shows 100% complete for bodies phase
		e.emitProgress(accountID, folderID, 1, 1, "bodies")
		e.pool.Release(conn)
		return nil
	}

	e.log.Info().Int("count", totalWithoutBody).Msg("Fetching message bodies (hybrid batch mode)")

	// Emit initial progress so frontend knows body fetch has started
	e.emitProgress(accountID, folderID, 0, totalWithoutBody, "bodies")

	// Tracking for error recovery and progress
	failedBatches := 0      // consecutive batch failures
	connectionFailures := 0 // total connection recovery attempts
	fetched := 0
	failed := 0

	// Track parse failures per message in this sync session
	// Messages that fail parsing (empty body) 3 times will be skipped for the rest of this session
	// This prevents infinite loops on messages that legitimately have no parseable body
	failedParseAttempts := make(map[string]int) // messageID -> attempt count
	const maxParseAttempts = 3

	// Processing result from goroutine - contains parsed data ready for DB
	type processingResult struct {
		bodyUpdates  []message.BodyUpdate
		attachments  []*message.Attachment
		fetchedCount int
	}

	// Channel and pending state for pipelined processing
	var pendingResultChan chan processingResult

	// Start heartbeat logging for long operations - shows sync is alive during long fetches
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				e.log.Info().
					Int("fetched", fetched).
					Int("total", totalWithoutBody).
					Int("failed", failed).
					Str("folder", f.Path).
					Msg("Body fetch in progress (heartbeat)")
			case <-heartbeatCtx.Done():
				return
			}
		}
	}()

	for {
		// Step 1: Wait for previous batch's goroutine (if any)
		// Step 2: Apply DB updates from previous batch
		if pendingResultChan != nil {
			e.log.Debug().Msg("Waiting for previous batch goroutine to complete")
			result := <-pendingResultChan
			e.log.Debug().
				Int("bodyUpdates", len(result.bodyUpdates)).
				Int("attachments", len(result.attachments)).
				Int("fetchedCount", result.fetchedCount).
				Msg("Received result from processing goroutine")

			// Track messages that parsed to empty body (potential parse failures)
			// These will be skipped after maxParseAttempts to prevent infinite loops
			for _, update := range result.bodyUpdates {
				if update.BodyHTML == "" && update.BodyText == "" {
					failedParseAttempts[update.MessageID]++
					if failedParseAttempts[update.MessageID] >= maxParseAttempts {
						e.log.Warn().
							Str("messageID", update.MessageID).
							Int("attempts", failedParseAttempts[update.MessageID]).
							Msg("Message body parsing failed after max attempts, skipping for this session")
					}
				}
			}

			// Apply database updates - MUST complete before querying next batch
			if len(result.bodyUpdates) > 0 {
				e.log.Debug().Int("count", len(result.bodyUpdates)).Msg("Applying batch DB update")
				if err := e.messageStore.UpdateBodiesBatch(result.bodyUpdates); err != nil {
					e.log.Warn().Err(err).Msg("Failed to batch update bodies")
					failed += result.fetchedCount
				} else {
					fetched += result.fetchedCount
					e.log.Debug().Int("fetched", fetched).Int("total", totalWithoutBody).Msg("DB update successful")
				}
			} else {
				e.log.Warn().Int("fetchedCount", result.fetchedCount).Msg("No body updates in result - bodies may be lost!")
			}
			if len(result.attachments) > 0 {
				if err := e.attachmentStore.CreateBatch(result.attachments); err != nil {
					e.log.Warn().Err(err).Msg("Failed to batch create attachments")
					// Attachments failed but bodies were saved, don't count as failed
				}
			}

			// Emit progress after DB update completes
			e.log.Debug().Int("fetched", fetched).Int("total", totalWithoutBody).Msg("Emitting progress")
			e.emitProgress(accountID, folderID, fetched, totalWithoutBody, "bodies")
			pendingResultChan = nil
		}

		// Check context before starting new batch
		if ctx.Err() != nil {
			e.log.Debug().Msg("Body fetch cancelled")
			e.pool.Release(conn)
			return ctx.Err()
		}

		// Step 3: Query candidates and build byte-aware batch
		// Get more candidates than we'll use to allow for byte-based selection
		candidates, err := e.messageStore.GetMessagesWithoutBodyAndSize(folderID, bodyBatchQueryLimit, sinceDate)
		if err != nil {
			e.pool.Release(conn)
			return fmt.Errorf("failed to get messages without body: %w", err)
		}

		e.log.Debug().
			Int("candidates", len(candidates)).
			Int("fetched", fetched).
			Int("failed", failed).
			Msg("Queried candidates for next batch")

		if len(candidates) == 0 {
			e.log.Debug().Msg("No more candidates, body sync complete")
			break // All done
		}

		// Filter out messages that have already failed parsing too many times this session
		var filteredCandidates []message.MessageWithSize
		for _, msg := range candidates {
			if failedParseAttempts[msg.ID] >= maxParseAttempts {
				continue // Skip - already failed too many times this session
			}
			filteredCandidates = append(filteredCandidates, msg)
		}

		// If all candidates have been filtered out, we're done
		if len(filteredCandidates) == 0 {
			e.log.Debug().
				Int("totalCandidates", len(candidates)).
				Int("skippedDueToRetries", len(candidates)).
				Msg("All remaining candidates have exceeded parse retry limit, finishing sync")
			break
		}

		// Adaptive batch sizing: use smaller batches for large mailboxes
		// This provides faster recovery if one batch fails and more frequent progress updates
		batchMaxMessages := bodyBatchMaxMessages
		batchMaxBytes := int64(bodyBatchMaxBytes)

		if totalWithoutBody > 1000 {
			batchMaxMessages = 25
			batchMaxBytes = 256 * 1024 // 256KB
			// Log only once (when we first enter the large mailbox mode)
			if fetched == 0 && failed == 0 {
				e.log.Info().
					Int("totalMessages", totalWithoutBody).
					Int("batchMaxMessages", batchMaxMessages).
					Int64("batchMaxBytes", batchMaxBytes).
					Msg("Using smaller batches for large mailbox")
			}
		}

		// Build batch using hybrid byte + count limits
		var batchIDs []string
		var batchBytes int64

		for _, msg := range filteredCandidates {
			msgSize := int64(msg.Size)
			if msgSize <= 0 {
				msgSize = 10 * 1024 // Assume 10KB for messages with unknown size
			}

			// Check if adding this message would exceed limits
			wouldExceedBytes := batchBytes+msgSize > batchMaxBytes && len(batchIDs) >= bodyBatchMinMessages
			wouldExceedCount := len(batchIDs) >= batchMaxMessages

			if wouldExceedBytes || wouldExceedCount {
				break // Batch is full
			}

			batchIDs = append(batchIDs, msg.ID)
			batchBytes += msgSize
		}

		if len(batchIDs) == 0 {
			e.log.Warn().Msg("No messages selected for batch")
			break
		}

		e.log.Debug().
			Int("batchSize", len(batchIDs)).
			Int64("batchBytes", batchBytes).
			Msg("Processing batch")

		// Get UIDs for all messages in batch (single DB query)
		uidInfos, err := e.messageStore.GetMessageUIDsAndFolder(batchIDs)
		if err != nil {
			e.log.Warn().Err(err).Msg("Failed to get UIDs for batch, skipping")
			failedBatches++
			if failedBatches > maxMessageRetries {
				e.log.Error().Int("failedBatches", failedBatches).Msg("Too many consecutive batch failures")
				break
			}
			continue
		}

		// Build UID -> messageID map for batch fetch
		uidToMessageID := make(map[uint32]string)
		for msgID, info := range uidInfos {
			uidToMessageID[info.UID] = msgID
		}

		if len(uidToMessageID) == 0 {
			e.log.Warn().Int("requested", len(batchIDs)).Msg("No valid UIDs found for batch")
			continue
		}

		// Step 4: Fetch bodies via IMAP - single round-trip for all messages in batch
		bodies, fetchErr := e.fetchMessageBodiesBatch(ctx, conn.Client().RawClient(), uidToMessageID)
		if fetchErr != nil {
			// Check if this is a connection error
			if imapPkg.IsConnectionError(fetchErr) {
				connectionFailures++

				// Check if we've exhausted connection recovery attempts
				if connectionFailures > maxConnectionRetries {
					e.log.Error().
						Int("connectionFailures", connectionFailures).
						Msg("Body fetch aborted - connection recovery failed")
					e.pool.Discard(conn)
					return fmt.Errorf("connection recovery failed after %d attempts", connectionFailures)
				}

				e.log.Debug().
					Err(fetchErr).
					Int("attempt", connectionFailures).
					Msg("Connection error during batch fetch, attempting recovery")

				// Discard dead connection and get a new one
				e.pool.Discard(conn)

				conn, err = e.pool.GetConnection(ctx, accountID)
				if err != nil {
					return fmt.Errorf("failed to get new connection after error: %w", err)
				}

				// Re-select mailbox on new connection
				_, err = conn.Client().SelectMailbox(ctx, f.Path)
				if err != nil {
					e.pool.Release(conn)
					return fmt.Errorf("failed to select mailbox on new connection: %w", err)
				}

				e.log.Debug().Msg("Connection recovered successfully, retrying batch")
				continue // Retry same batch
			}

			// Non-connection error
			e.log.Warn().Err(fetchErr).Msg("Batch fetch failed with non-connection error")
			failedBatches++
			if failedBatches > maxMessageRetries {
				e.log.Error().Int("failedBatches", failedBatches).Msg("Too many consecutive batch failures")
				break
			}
			continue
		}

		// Reset failure counters on success
		failedBatches = 0

		// If we got no bodies back, mark all messages in this batch as failed
		// to prevent infinite loop (same messages being queried over and over)
		if len(bodies) == 0 {
			e.log.Warn().Int("requested", len(uidToMessageID)).Msg("IMAP returned no bodies for batch")
			// Mark all messages in this batch as having failed parse attempts
			// This prevents them from being selected again in this sync session
			for _, msgID := range batchIDs {
				failedParseAttempts[msgID] = maxParseAttempts // Mark as max failures to skip
			}
			failed += len(uidToMessageID)
			continue
		}

		// Step 5: Launch goroutine to build body updates
		// DB update will happen in step 2 of the NEXT iteration
		// Attachments were already extracted during parsing - no re-parse needed!
		resultChan := make(chan processingResult, 1)
		currentBodies := bodies // capture for goroutine

		go func() {
			startTime := time.Now()
			var bodyUpdates []message.BodyUpdate
			var allAttachments []*message.Attachment

			for _, pb := range currentBodies {
				// Build body update
				bu := message.BodyUpdate{
					MessageID:      pb.MessageID,
					BodyHTML:       pb.BodyHTML,
					BodyText:       pb.BodyText,
					Snippet:        pb.Snippet,
					SMIMERawBody:   pb.SMIMERawBody,
					SMIMEEncrypted: pb.SMIMEEncrypted,
					PGPRawBody:     pb.PGPRawBody,
					PGPEncrypted:   pb.PGPEncrypted,
				}
				// Don't cache S/MIME or PGP verification status — computed fresh on each view
				bodyUpdates = append(bodyUpdates, bu)

				// Use pre-extracted attachments (no re-parsing!)
				if len(pb.Attachments) > 0 {
					allAttachments = append(allAttachments, pb.Attachments...)
				}
			}

			e.log.Debug().
				Int("bodyUpdates", len(bodyUpdates)).
				Int("attachments", len(allAttachments)).
				Dur("elapsed", time.Since(startTime)).
				Msg("Built body updates and attachments for batch")

			resultChan <- processingResult{
				bodyUpdates:  bodyUpdates,
				attachments:  allAttachments,
				fetchedCount: len(currentBodies),
			}
		}()

		// Mark that we have pending work - will be processed in step 1-2 of next iteration
		pendingResultChan = resultChan
	}

	// Handle final batch if there's pending work
	if pendingResultChan != nil {
		result := <-pendingResultChan

		// Track messages that parsed to empty body (for logging purposes on final batch)
		for _, update := range result.bodyUpdates {
			if update.BodyHTML == "" && update.BodyText == "" {
				failedParseAttempts[update.MessageID]++
				if failedParseAttempts[update.MessageID] >= maxParseAttempts {
					e.log.Warn().
						Str("messageID", update.MessageID).
						Int("attempts", failedParseAttempts[update.MessageID]).
						Msg("Message body parsing failed after max attempts, skipping for this session")
				}
			}
		}

		if len(result.bodyUpdates) > 0 {
			if err := e.messageStore.UpdateBodiesBatch(result.bodyUpdates); err != nil {
				e.log.Warn().Err(err).Msg("Failed to batch update bodies (final)")
				failed += result.fetchedCount
			} else {
				fetched += result.fetchedCount
			}
		}
		if len(result.attachments) > 0 {
			if err := e.attachmentStore.CreateBatch(result.attachments); err != nil {
				e.log.Warn().Err(err).Msg("Failed to batch create attachments (final)")
			}
		}

		e.emitProgress(accountID, folderID, fetched, totalWithoutBody, "bodies")
	}

	// Release connection when done
	e.pool.Release(conn)

	// Log summary
	if failed > 0 {
		e.log.Info().
			Int("fetched", fetched).
			Int("failed", failed).
			Int("total", totalWithoutBody).
			Msg("Body fetch complete with failures (hybrid batch mode)")
	} else {
		e.log.Info().
			Int("fetched", fetched).
			Int("total", totalWithoutBody).
			Msg("Body fetch complete (hybrid batch mode)")
	}

	return nil
}

// fetchAllUIDs fetches all UIDs from the currently selected mailbox.
// Uses a goroutine to allow context cancellation since Wait() blocks indefinitely.
func (e *Engine) fetchAllUIDs(ctx context.Context, client *imapclient.Client) ([]uint32, error) {
	e.log.Debug().Msg("Fetching all UIDs from server")

	// UID Search for all messages (must use UIDSearch to get UIDs, not sequence numbers)
	searchCmd := client.UIDSearch(&imap.SearchCriteria{}, nil)

	// Run Wait() in a goroutine to allow context cancellation
	type searchResult struct {
		data *imap.SearchData
		err  error
	}
	resultCh := make(chan searchResult, 1)
	go func() {
		data, err := searchCmd.Wait()
		resultCh <- searchResult{data, err}
	}()

	// Wait for either result or context cancellation
	select {
	case <-ctx.Done():
		e.log.Debug().Msg("UID search cancelled by context")
		return nil, ctx.Err()
	case result := <-resultCh:
		if result.err != nil {
			e.log.Error().Err(result.err).Msg("UID search command failed")
			return nil, fmt.Errorf("UID search failed: %w", result.err)
		}

		var uids []uint32
		for _, uid := range result.data.AllUIDs() {
			uids = append(uids, uint32(uid))
		}

		return uids, nil
	}
}

// messageWithRaw holds a message and its raw bytes for attachment extraction
type messageWithRaw struct {
	msg *message.Message
	raw []byte
}

// fetchMessages fetches message envelopes for the given UIDs.
// Uses streaming (Next() loop) instead of Collect() to:
// - Avoid indefinite blocking if connection hangs
// - Allow context cancellation between messages
// - Return partial results if connection dies mid-batch
func (e *Engine) fetchMessages(ctx context.Context, client *imapclient.Client, accountID, folderID string, uids []uint32) error {
	if len(uids) == 0 {
		return nil
	}

	e.log.Debug().Int("count", len(uids)).Msg("Fetching message envelopes")

	// Convert to imap.UIDSet
	uidSet := imap.UIDSet{}
	for _, uid := range uids {
		uidSet.AddNum(imap.UID(uid))
	}

	// Fetch envelope, flags, size, and full body
	fetchOptions := &imap.FetchOptions{
		Envelope:   true,
		Flags:      true,
		RFC822Size: true,
		UID:        true,
		BodySection: []*imap.FetchItemBodySection{
			{
				Specifier: imap.PartSpecifierNone, // Fetch entire message (headers + body)
				Peek:      true,
			},
		},
	}

	fetchCmd := client.Fetch(uidSet, fetchOptions)

	// Stream messages one at a time instead of blocking on Collect()
	var savedMessages []messageWithRaw
	fetchedCount := 0

	for {
		// Check for cancellation between messages
		if ctx.Err() != nil {
			fetchCmd.Close()
			e.log.Warn().
				Int("fetched", fetchedCount).
				Int("requested", len(uids)).
				Msg("Message fetch cancelled, saved partial results")
			break
		}

		msg := fetchCmd.Next()
		if msg == nil {
			break
		}

		// Collect all data items for this message
		var fetchedUID imap.UID
		var envelope *imap.Envelope
		var flags []imap.Flag
		var rfc822Size int64
		var rawBytes []byte

		for {
			item := msg.Next()
			if item == nil {
				break
			}

			switch data := item.(type) {
			case imapclient.FetchItemDataUID:
				fetchedUID = data.UID
			case imapclient.FetchItemDataEnvelope:
				envelope = data.Envelope
			case imapclient.FetchItemDataFlags:
				flags = data.Flags
			case imapclient.FetchItemDataRFC822Size:
				rfc822Size = data.Size
			case imapclient.FetchItemDataBodySection:
				// Read body bytes from literal reader with size limit
				if data.Literal != nil {
					lr := io.LimitReader(data.Literal, maxMessageSize)
					var err error
					rawBytes, err = io.ReadAll(lr)
					if err != nil {
						e.log.Warn().Err(err).Uint32("uid", uint32(fetchedUID)).Msg("Failed to read body literal")
					}
				}
			}
		}

		if fetchedUID == 0 {
			e.log.Warn().Msg("Received message without UID in fetch")
			continue
		}

		// Build message from streamed data
		m := e.buildMessageFromStreamedData(accountID, folderID, fetchedUID, envelope, flags, rfc822Size, rawBytes)

		// Save to store
		if err := e.messageStore.Create(m); err != nil {
			e.log.Warn().Err(err).Uint32("uid", m.UID).Msg("Failed to save message")
			continue
		}
		savedMessages = append(savedMessages, messageWithRaw{msg: m, raw: rawBytes})
		fetchedCount++

		// Extract and store attachment metadata (if attachments exist)
		if m.HasAttachments && len(rawBytes) > 0 && e.attachmentStore != nil {
			attachments, err := e.attachExtractor.ExtractAttachments(m.ID, rawBytes)
			if err != nil {
				e.log.Debug().Err(err).Str("messageId", m.ID).Msg("Failed to extract attachments")
			} else {
				for _, att := range attachments {
					// For inline attachments, store the content for offline access
					if att.Attachment.IsInline && len(att.Content) > 0 {
						att.Attachment.Content = att.Content
					}
					if err := e.attachmentStore.Create(att.Attachment); err != nil {
						e.log.Debug().Err(err).Str("filename", att.Attachment.Filename).Msg("Failed to save attachment metadata")
					}
				}
			}
		}
	}

	// Close fetch command
	if err := fetchCmd.Close(); err != nil {
		e.log.Warn().Err(err).
			Int("fetched", fetchedCount).
			Int("requested", len(uids)).
			Msg("Fetch close error, continuing with partial results")
	}

	// Second pass: compute and update thread IDs
	// This needs to happen after all messages are saved so we can find related messages
	for _, mwr := range savedMessages {
		m := mwr.msg
		threadID := e.computeThreadID(accountID, m)
		if threadID != "" && threadID != m.ThreadID {
			m.ThreadID = threadID
			if err := e.messageStore.UpdateThreadID(m.ID, threadID); err != nil {
				e.log.Warn().Err(err).Str("messageId", m.ID).Msg("Failed to update thread ID")
			}
		}

		// Reconcile threads: link this message with related messages
		// This handles cases where replies were synced before the original message
		if err := e.messageStore.ReconcileThreadsForNewMessage(accountID, m.ID, m.MessageID, m.ThreadID, m.InReplyTo); err != nil {
			e.log.Warn().Err(err).Str("messageId", m.ID).Msg("Failed to reconcile threads")
		}
	}

	return nil
}

// buildMessageFromStreamedData constructs a Message from streamed IMAP data
func (e *Engine) buildMessageFromStreamedData(accountID, folderID string, uid imap.UID, envelope *imap.Envelope, flags []imap.Flag, rfc822Size int64, rawBytes []byte) *message.Message {
	m := &message.Message{
		AccountID:  accountID,
		FolderID:   folderID,
		UID:        uint32(uid),
		ReceivedAt: time.Now().UTC(),
		Size:       int(rfc822Size),
	}

	// Parse envelope
	var references []string
	if envelope != nil {
		m.Subject = envelope.Subject
		m.MessageID = envelope.MessageID
		if len(envelope.InReplyTo) > 0 {
			m.InReplyTo = envelope.InReplyTo[0]
		}
		m.Date = envelope.Date.UTC()

		// From
		if len(envelope.From) > 0 {
			m.FromName = envelope.From[0].Name
			m.FromEmail = envelope.From[0].Addr()
		}

		// To
		if len(envelope.To) > 0 {
			m.ToList = addressListToJSON(envelope.To)
		}

		// Cc
		if len(envelope.Cc) > 0 {
			m.CcList = addressListToJSON(envelope.Cc)
		}

		// Reply-To
		if len(envelope.ReplyTo) > 0 {
			m.ReplyTo = envelope.ReplyTo[0].Addr()
		}
	}

	// Extract References and Disposition-Notification-To from raw message
	if len(rawBytes) > 0 {
		references = e.extractReferences(rawBytes)
		m.ReadReceiptTo = e.extractDispositionNotificationTo(rawBytes)
	}

	// Store references as JSON array
	if len(references) > 0 {
		refsJSON, _ := json.Marshal(references)
		m.References = string(refsJSON)
	}

	// Parse flags
	for _, flag := range flags {
		switch flag {
		case imap.FlagSeen:
			m.IsRead = true
		case imap.FlagFlagged:
			m.IsStarred = true
		case imap.FlagAnswered:
			m.IsAnswered = true
		case imap.FlagDraft:
			m.IsDraft = true
		case imap.FlagDeleted:
			m.IsDeleted = true
		case "$Forwarded", "\\Forwarded":
			m.IsForwarded = true
		}
	}

	// Parse message body
	if len(rawBytes) > 0 {
		bodyText, bodyHTML, hasAttachments := e.parseMessageBody(rawBytes)
		m.BodyText = bodyText
		m.HasAttachments = hasAttachments

		// Sanitize HTML
		if bodyHTML != "" {
			m.BodyHTML = e.sanitizer.Sanitize(bodyHTML)
		}

		// Generate snippet
		if bodyText != "" {
			m.Snippet = generateSnippet(bodyText, 200)
		} else if bodyHTML != "" {
			m.Snippet = generateSnippet(stripHTMLTags(bodyHTML), 200)
		}
	}

	return m
}

// parseMessageBuffer converts an IMAP FetchMessageBuffer to a Message
func (e *Engine) parseMessageBuffer(accountID, folderID string, buf *imapclient.FetchMessageBuffer) (*message.Message, error) {
	m := &message.Message{
		AccountID:  accountID,
		FolderID:   folderID,
		UID:        uint32(buf.UID),
		ReceivedAt: time.Now().UTC(),
	}

	// Parse envelope
	envelope := buf.Envelope
	var references []string
	if envelope != nil {
		m.Subject = envelope.Subject
		m.MessageID = envelope.MessageID
		if len(envelope.InReplyTo) > 0 {
			m.InReplyTo = envelope.InReplyTo[0]
		}
		m.Date = envelope.Date.UTC() // Normalize to UTC for consistent sorting

		// From
		if len(envelope.From) > 0 {
			m.FromName = envelope.From[0].Name
			m.FromEmail = envelope.From[0].Addr()
		}

		// To
		if len(envelope.To) > 0 {
			m.ToList = addressListToJSON(envelope.To)
		}

		// Cc
		if len(envelope.Cc) > 0 {
			m.CcList = addressListToJSON(envelope.Cc)
		}

		// Reply-To
		if len(envelope.ReplyTo) > 0 {
			m.ReplyTo = envelope.ReplyTo[0].Addr()
		}
	}

	// Extract References header and Disposition-Notification-To from raw message
	for _, section := range buf.BodySection {
		if len(section.Bytes) > 0 {
			references = e.extractReferences(section.Bytes)
			m.ReadReceiptTo = e.extractDispositionNotificationTo(section.Bytes)
			break
		}
	}

	// Store references as JSON array
	if len(references) > 0 {
		refsJSON, _ := json.Marshal(references)
		m.References = string(refsJSON)
	}

	// Parse flags
	for _, flag := range buf.Flags {
		switch flag {
		case imap.FlagSeen:
			m.IsRead = true
		case imap.FlagFlagged:
			m.IsStarred = true
		case imap.FlagAnswered:
			m.IsAnswered = true
		case imap.FlagDraft:
			m.IsDraft = true
		case imap.FlagDeleted:
			m.IsDeleted = true
		case "$Forwarded", "\\Forwarded":
			m.IsForwarded = true
		}
	}

	// Size
	m.Size = int(buf.RFC822Size)

	// Parse message body from fetched data
	for _, section := range buf.BodySection {
		if len(section.Bytes) > 0 {
			e.log.Debug().
				Int("rawBodyLen", len(section.Bytes)).
				Str("messageID", m.MessageID).
				Str("subject", m.Subject).
				Msg("Parsing message body from section")

			bodyText, bodyHTML, hasAttachments := e.parseMessageBody(section.Bytes)
			m.BodyText = bodyText

			// Sanitize HTML to prevent XSS
			if bodyHTML != "" {
				m.BodyHTML = e.sanitizer.Sanitize(bodyHTML)
			}

			m.HasAttachments = hasAttachments

			e.log.Debug().
				Int("bodyTextLen", len(m.BodyText)).
				Int("bodyHTMLLen", len(m.BodyHTML)).
				Bool("hasAttachments", m.HasAttachments).
				Str("messageID", m.MessageID).
				Msg("Parsed message body")

			// Generate snippet from plain text body
			if bodyText != "" {
				m.Snippet = generateSnippet(bodyText, 200)
			} else if bodyHTML != "" {
				// Strip HTML tags for snippet
				m.Snippet = generateSnippet(stripHTMLTags(bodyHTML), 200)
			}
			break
		}
	}

	return m, nil
}

// parseMessageBody parses a raw email message and extracts text/plain and text/html parts
func (e *Engine) parseMessageBody(raw []byte) (bodyText, bodyHTML string, hasAttachments bool) {
	reader := bytes.NewReader(raw)

	// Parse the message using go-message
	entity, err := gomessage.Read(reader)
	if err != nil {
		e.log.Debug().Err(err).Int("rawLen", len(raw)).Msg("Failed to parse message, trying as plain text")
		// If parsing fails, treat entire content as plain text
		return string(raw), "", false
	}

	// Log top-level Content-Type for debugging
	topLevelCT := entity.Header.Get("Content-Type")
	e.log.Debug().
		Str("topLevelContentType", topLevelCT).
		Int("rawLen", len(raw)).
		Msg("Parsing message body")

	// Check if it's a multipart message
	mr := entity.MultipartReader()
	e.log.Debug().Bool("isMultipart", mr != nil).Msg("Multipart detection result")

	if mr != nil {
		// Multipart message - iterate through parts
		partIndex := 0
		for {
			part, err := mr.NextPart()
			if err != nil {
				// EOF (or wrapped EOF like "multipart: NextPart: EOF") signals end of parts
				if !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "EOF") {
					e.log.Debug().Err(err).Int("partsProcessed", partIndex).Msg("Error reading multipart")
				} else {
					e.log.Debug().Int("partsProcessed", partIndex).Msg("Finished reading multipart parts")
				}
				break
			}
			partIndex++

			contentType, params, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			disposition, _, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))

			e.log.Debug().
				Int("partIndex", partIndex).
				Str("contentType", contentType).
				Str("disposition", disposition).
				Str("charset", params["charset"]).
				Msg("Processing multipart part")

			// Check for attachments
			if disposition == "attachment" {
				hasAttachments = true
				continue
			}

			// Handle nested multipart
			if strings.HasPrefix(contentType, "multipart/") {
				nestedText, nestedHTML, nestedAttach := e.parseNestedMultipart(part)
				if bodyText == "" {
					bodyText = nestedText
				}
				if bodyHTML == "" {
					bodyHTML = nestedHTML
				}
				hasAttachments = hasAttachments || nestedAttach
				continue
			}

			// Read the part body with size limit to prevent memory exhaustion
			lr := io.LimitReader(part.Body, maxPartSize)
			partBody, err := io.ReadAll(lr)
			if err != nil {
				// Check if we got partial data despite the error (e.g., malformed email missing closing boundary)
				if len(partBody) > 0 {
					e.log.Warn().
						Err(err).
						Int("partIndex", partIndex).
						Int("partialLen", len(partBody)).
						Msg("Read partial part body despite error, using partial data")
					// Continue processing with partial data (don't skip)
				} else {
					e.log.Debug().Err(err).Int("partIndex", partIndex).Msg("Failed to read part body, no data recovered")
					continue
				}
			}

			// Log if we hit the size limit (truncated)
			if int64(len(partBody)) == maxPartSize {
				e.log.Warn().
					Int("partIndex", partIndex).
					Int64("maxSize", maxPartSize).
					Msg("Part body truncated at size limit - saving partial content")
			}

			e.log.Debug().
				Int("partIndex", partIndex).
				Int("partBodyLen", len(partBody)).
				Msg("Read part body successfully")

			// First, check if content needs explicit quoted-printable decoding
			// (go-message should handle this via Entity.Body, but some edge cases might slip through)
			partBody = decodeQuotedPrintableIfNeeded(partBody)

			// Decode charset to UTF-8
			charset := params["charset"]

			// If no charset in header and this is HTML, try to extract from meta tags
			if charset == "" && contentType == "text/html" {
				charset = extractCharsetFromHTML(partBody)
				e.log.Debug().
					Str("charsetFromHTML", charset).
					Msg("Extracted charset from HTML meta tags")
			}
			decodedContent := decodeCharset(partBody, charset)

			// Debug: Check if content still contains quoted-printable sequences
			if contentType == "text/html" && len(decodedContent) > 200 {
				snippet := decodedContent
				if len(snippet) > 200 {
					snippet = snippet[:200]
				}
				e.log.Debug().
					Str("htmlSnippet", snippet).
					Bool("hasQuotedPrintable", strings.Contains(decodedContent, "=3D")).
					Msg("HTML content analysis")
			}

			switch contentType {
			case "text/plain":
				if bodyText == "" {
					bodyText = decodedContent
				}
			case "text/html":
				if bodyHTML == "" {
					bodyHTML = decodedContent
				}
			default:
				// Other content types might be inline attachments
				if disposition == "inline" && strings.HasPrefix(contentType, "image/") {
					// Inline images need to be extracted so they can be displayed
					hasAttachments = true
				} else if contentType != "" && !strings.HasPrefix(contentType, "text/") {
					hasAttachments = true
				}
			}
		}
	} else {
		// Single part message
		contentType, params, _ := mime.ParseMediaType(entity.Header.Get("Content-Type"))
		e.log.Debug().
			Str("contentType", contentType).
			Str("charset", params["charset"]).
			Msg("Processing single-part message")

		// Read with size limit to prevent memory exhaustion
		lr := io.LimitReader(entity.Body, maxPartSize)
		body, err := io.ReadAll(lr)
		if err != nil {
			e.log.Debug().Err(err).Msg("Failed to read single-part message body")
			return "", "", false
		}

		// Log if we hit the size limit (truncated)
		if int64(len(body)) == maxPartSize {
			e.log.Warn().
				Int64("maxSize", maxPartSize).
				Msg("Single-part body truncated at size limit - saving partial content")
		}

		e.log.Debug().Int("bodyLen", len(body)).Msg("Read single-part message body")

		// First, check if content needs explicit quoted-printable decoding
		body = decodeQuotedPrintableIfNeeded(body)

		// Decode charset to UTF-8
		charset := params["charset"]
		// If no charset in header and this is HTML, try to extract from meta tags
		if charset == "" && contentType == "text/html" {
			charset = extractCharsetFromHTML(body)
		}
		decodedContent := decodeCharset(body, charset)

		e.log.Debug().Int("decodedLen", len(decodedContent)).Msg("Decoded single-part content")

		switch contentType {
		case "text/html":
			bodyHTML = decodedContent
		default:
			// Default to plain text
			bodyText = decodedContent
		}
	}

	// Log final result
	e.log.Debug().
		Int("bodyTextLen", len(bodyText)).
		Int("bodyHTMLLen", len(bodyHTML)).
		Bool("hasAttachments", hasAttachments).
		Msg("parseMessageBody complete")

	return bodyText, bodyHTML, hasAttachments
}

// parseMessageBodyWithTimeout parses message body with a timeout.
// If parsing takes too long (potentially due to malformed emails), it returns
// partial results via fallback extraction - better than nothing.
func (e *Engine) parseMessageBodyWithTimeout(raw []byte, timeout time.Duration) (bodyText, bodyHTML string, hasAttachments bool) {
	result := e.parseMessageBodyFull(raw, "", timeout)
	return result.BodyText, result.BodyHTML, result.HasAttachments
}

// parseMessageBodyFull parses a raw email and extracts text, HTML, and attachment metadata.
// Attachments are extracted during the same parsing pass - no re-parsing needed.
// For inline images, content is also captured (up to maxInlineContentSize) for display.
// For file attachments, only metadata is captured - content fetched on-demand.
// messageID is needed to create attachment records.
func (e *Engine) parseMessageBodyFull(raw []byte, messageID string, timeout time.Duration) *ParsedBody {
	type result struct {
		parsed *ParsedBody
	}

	// Use buffered channel to prevent goroutine leak if timeout fires
	done := make(chan result, 1)

	go func() {
		parsed := e.parseMessageBodyInternal(raw, messageID)
		select {
		case done <- result{parsed}:
		default:
		}
	}()

	select {
	case r := <-done:
		return r.parsed
	case <-time.After(timeout):
		e.log.Warn().
			Int("rawLen", len(raw)).
			Dur("timeout", timeout).
			Msg("Body parsing timed out - attempting fallback extraction")

		partialText := e.extractPlainTextFallback(raw)
		return &ParsedBody{
			BodyText:       partialText,
			BodyHTML:       "",
			HasAttachments: false,
			Attachments:    nil,
		}
	}
}

// parseMessageBodyInternal does the actual parsing work, extracting body text, HTML, and attachments.
func (e *Engine) parseMessageBodyInternal(raw []byte, messageID string) *ParsedBody {
	result := &ParsedBody{}
	reader := bytes.NewReader(raw)

	entity, err := gomessage.Read(reader)
	if err != nil {
		e.log.Debug().Err(err).Int("rawLen", len(raw)).Msg("Failed to parse message, trying as plain text")
		result.BodyText = string(raw)
		return result
	}

	topLevelCT := entity.Header.Get("Content-Type")
	e.log.Debug().
		Str("topLevelContentType", topLevelCT).
		Int("rawLen", len(raw)).
		Msg("Parsing message body")

	// Check for S/MIME content (signed or encrypted)
	isSigned := smime.IsSMIMESigned(topLevelCT)
	isEncrypted := smime.IsSMIMEEncrypted(topLevelCT)

	if isSigned || isEncrypted {
		// Store raw body for on-view processing (verification/decryption happens fresh on each view)
		result.SMIMERawBody = raw
		result.SMIMEEncrypted = isEncrypted

		if isEncrypted {
			// Encrypted: don't store body text/html (decrypted only on view)
			return result
		}

		// Signed-only: still parse body for FTS, but don't cache verification status
		if e.smimeVerifier != nil {
			_, innerBody := e.smimeVerifier.VerifyAndUnwrap(raw)
			// Use the unwrapped inner body for parsing (not the S/MIME wrapper)
			if innerBody != nil {
				raw = innerBody
				reader = bytes.NewReader(raw)
				newEntity, parseErr := gomessage.Read(reader)
				if parseErr != nil {
					e.log.Debug().Err(parseErr).Msg("Failed to re-parse unwrapped S/MIME body")
					result.BodyText = string(raw)
					return result
				}
				entity = newEntity
				topLevelCT = entity.Header.Get("Content-Type")
			}
		}
	}

	// Check for PGP/MIME content (signed or encrypted)
	isPGPSigned := pgp.IsPGPSigned(topLevelCT)
	isPGPEncrypted := pgp.IsPGPEncrypted(topLevelCT)

	if isPGPSigned || isPGPEncrypted {
		// Store raw body for on-view processing (verification/decryption happens fresh on each view)
		result.PGPRawBody = raw
		result.PGPEncrypted = isPGPEncrypted

		if isPGPEncrypted {
			// Encrypted: don't store body text/html (decrypted only on view)
			return result
		}

		// Signed-only: still parse body for FTS, but don't cache verification status
		if e.pgpVerifier != nil {
			_, innerBody := e.pgpVerifier.VerifyAndUnwrap(raw)
			// Use the unwrapped inner body for parsing (not the PGP wrapper)
			if innerBody != nil {
				raw = innerBody
				reader = bytes.NewReader(raw)
				newEntity, parseErr := gomessage.Read(reader)
				if parseErr != nil {
					e.log.Debug().Err(parseErr).Msg("Failed to re-parse unwrapped PGP body")
					result.BodyText = string(raw)
					return result
				}
				entity = newEntity
			}
		}
	}

	mr := entity.MultipartReader()
	e.log.Debug().Bool("isMultipart", mr != nil).Msg("Multipart detection result")

	if mr != nil {
		e.parseMultipartBody(mr, result, messageID)
	} else {
		e.parseSinglePartBody(entity, result)
	}

	e.log.Debug().
		Int("bodyTextLen", len(result.BodyText)).
		Int("bodyHTMLLen", len(result.BodyHTML)).
		Bool("hasAttachments", result.HasAttachments).
		Int("attachmentCount", len(result.Attachments)).
		Msg("parseMessageBody complete")

	return result
}

// parseMultipartBody parses a multipart message body
func (e *Engine) parseMultipartBody(mr gomessage.MultipartReader, result *ParsedBody, messageID string) {
	partIndex := 0
	for {
		part, err := mr.NextPart()
		if err != nil {
			if !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "EOF") {
				e.log.Debug().Err(err).Int("partsProcessed", partIndex).Msg("Error reading multipart")
			} else {
				e.log.Debug().Int("partsProcessed", partIndex).Msg("Finished reading multipart parts")
			}
			break
		}
		partIndex++

		contentType, params, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		disposition, dispParams, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		contentID := strings.Trim(part.Header.Get("Content-ID"), "<>")

		e.log.Debug().
			Int("partIndex", partIndex).
			Str("contentType", contentType).
			Str("disposition", disposition).
			Str("charset", params["charset"]).
			Msg("Processing multipart part")

		// Handle file attachments
		if disposition == "attachment" {
			result.HasAttachments = true
			// If the attachment has a Content-ID, it's meant to be displayed inline in the HTML
			// (referenced via cid:contentID), even if Content-Disposition says "attachment"
			isInline := contentID != ""
			att := e.extractAttachmentMetadata(part, messageID, contentType, dispParams, contentID, isInline)
			if att != nil {
				result.Attachments = append(result.Attachments, att)
			}
			continue
		}

		// Handle nested multipart
		if strings.HasPrefix(contentType, "multipart/") {
			if nestedMr := part.MultipartReader(); nestedMr != nil {
				e.parseMultipartBody(nestedMr, result, messageID)
			}
			continue
		}

		// Handle inline images (explicit inline disposition OR image with Content-ID)
		// Many emails have images with Content-ID but no Content-Disposition header
		if (disposition == "inline" && strings.HasPrefix(contentType, "image/")) ||
			(contentID != "" && strings.HasPrefix(contentType, "image/")) {
			result.HasAttachments = true
			att := e.extractAttachmentMetadata(part, messageID, contentType, dispParams, contentID, true)
			if att != nil {
				result.Attachments = append(result.Attachments, att)
			}
			continue
		}

		// Read text/html parts
		lr := io.LimitReader(part.Body, maxPartSize)
		partBody, err := io.ReadAll(lr)
		if err != nil {
			if len(partBody) > 0 {
				e.log.Warn().Err(err).Int("partIndex", partIndex).Int("partialLen", len(partBody)).Msg("Read partial part body")
			} else {
				e.log.Debug().Err(err).Int("partIndex", partIndex).Msg("Failed to read part body")
				continue
			}
		}

		if int64(len(partBody)) == maxPartSize {
			e.log.Warn().Int("partIndex", partIndex).Int64("maxSize", maxPartSize).Msg("Part body truncated")
		}

		e.log.Debug().Int("partIndex", partIndex).Int("partBodyLen", len(partBody)).Msg("Read part body successfully")

		charset := params["charset"]
		if charset == "" && contentType == "text/html" {
			charset = extractCharsetFromHTML(partBody)
		}
		decodedContent := decodeCharset(partBody, charset)

		switch contentType {
		case "text/plain":
			if result.BodyText == "" {
				result.BodyText = decodedContent
			}
		case "text/html":
			if result.BodyHTML == "" {
				result.BodyHTML = decodedContent
			}
		default:
			// Other content types might be implicit attachments
			if contentType != "" && !strings.HasPrefix(contentType, "text/") {
				result.HasAttachments = true
			}
		}
	}
}

// parseSinglePartBody parses a single-part message body
func (e *Engine) parseSinglePartBody(entity *gomessage.Entity, result *ParsedBody) {
	contentType, params, _ := mime.ParseMediaType(entity.Header.Get("Content-Type"))
	e.log.Debug().Str("contentType", contentType).Str("charset", params["charset"]).Msg("Processing single-part message")

	lr := io.LimitReader(entity.Body, maxPartSize)
	body, err := io.ReadAll(lr)
	if err != nil {
		e.log.Debug().Err(err).Msg("Failed to read single-part message body")
		return
	}

	if int64(len(body)) == maxPartSize {
		e.log.Warn().Int64("maxSize", maxPartSize).Msg("Single-part body truncated")
	}

	e.log.Debug().Int("bodyLen", len(body)).Msg("Read single-part message body")

	charset := params["charset"]
	if charset == "" && contentType == "text/html" {
		charset = extractCharsetFromHTML(body)
	}
	decodedContent := decodeCharset(body, charset)

	e.log.Debug().Int("decodedLen", len(decodedContent)).Msg("Decoded single-part content")

	switch contentType {
	case "text/html":
		result.BodyHTML = decodedContent
	default:
		result.BodyText = decodedContent
	}
}

// decodeMIMEWord decodes RFC 2047 encoded words (e.g., =?UTF-8?B?5Lit5paH?=)
// used for non-ASCII filenames and headers
func decodeMIMEWord(s string) string {
	if s == "" {
		return s
	}
	// Use mime.WordDecoder with charset fallback support
	dec := &mime.WordDecoder{
		CharsetReader: func(charsetName string, r io.Reader) (io.Reader, error) {
			// First try the go-message charset package
			if reader, err := msgcharset.Reader(charsetName, r); err == nil {
				return reader, nil
			}
			// Fall back to htmlindex for broader charset support (GB2312, GBK, Big5, etc.)
			enc, err := htmlindex.Get(charsetName)
			if err != nil {
				return nil, fmt.Errorf("unknown charset: %s", charsetName)
			}
			return enc.NewDecoder().Reader(r), nil
		},
	}
	decoded, err := dec.DecodeHeader(s)
	if err != nil {
		// If decoding fails, return original string
		return s
	}
	return decoded
}

// extractAttachmentMetadata extracts attachment metadata from a MIME part.
// For inline images, also captures content (up to maxInlineContentSize).
// For file attachments, reads content to get size but doesn't store it (fetched on-demand).
func (e *Engine) extractAttachmentMetadata(part *gomessage.Entity, messageID, contentType string, dispParams map[string]string, contentID string, isInline bool) *message.Attachment {
	filename := dispParams["filename"]
	if filename == "" {
		// Try to get from Content-Type name parameter
		_, ctParams, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		filename = ctParams["name"]
	}
	// Decode RFC 2047 encoded filenames (e.g., =?UTF-8?B?5Lit5paH?= for Chinese)
	filename = decodeMIMEWord(filename)
	if filename == "" {
		// Generate a filename based on content type
		ext := ".bin"
		if strings.HasPrefix(contentType, "image/") {
			parts := strings.Split(contentType, "/")
			if len(parts) == 2 {
				ext = "." + parts[1]
			}
		}
		filename = "attachment" + ext
	}

	att := &message.Attachment{
		ID:          generateID(),
		MessageID:   messageID,
		Filename:    filename,
		ContentType: contentType,
		ContentID:   contentID,
		IsInline:    isInline,
	}

	// Read the attachment content
	lr := io.LimitReader(part.Body, maxPartSize)
	content, err := io.ReadAll(lr)
	if err != nil {
		e.log.Debug().Err(err).Str("filename", filename).Msg("Failed to read attachment content")
		return att
	}

	att.Size = len(content)

	if isInline {
		// For inline images, store content (needed for display in email)
		// But limit to maxInlineContentSize to prevent huge DB entries
		if len(content) <= maxInlineContentSize {
			att.Content = content
			e.log.Debug().Str("filename", filename).Int("size", len(content)).Msg("Extracted inline attachment with content")
		} else {
			e.log.Debug().Str("filename", filename).Int("size", len(content)).Msg("Inline attachment too large, stored metadata only")
		}
	} else {
		// For file attachments, we have the size but don't store content
		// Content will be fetched on-demand when user downloads
		e.log.Debug().Str("filename", filename).Int("size", len(content)).Msg("Extracted file attachment metadata")
	}

	return att
}

// generateID generates a unique ID for attachments
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// extractPlainTextFallback attempts to extract readable text from raw email bytes.
// Used when normal parsing times out or fails completely.
// Returns partial content which is better than nothing.
func (e *Engine) extractPlainTextFallback(raw []byte) string {
	rawStr := string(raw)

	// Find the body (after double CRLF or double LF - standard email header/body separator)
	bodyStart := strings.Index(rawStr, "\r\n\r\n")
	if bodyStart == -1 {
		bodyStart = strings.Index(rawStr, "\n\n")
	}
	if bodyStart == -1 {
		// No header/body separator found, can't extract safely
		return ""
	}

	body := rawStr[bodyStart+4:]

	// Extract printable ASCII characters as a last resort
	// This handles cases where content might be partially encoded
	var result strings.Builder
	for _, r := range body {
		if r >= 32 && r < 127 || r == '\n' || r == '\r' || r == '\t' {
			result.WriteRune(r)
		}
	}

	text := strings.TrimSpace(result.String())

	// Limit to first 10KB to prevent huge partial extractions
	const maxFallbackSize = 10 * 1024
	if len(text) > maxFallbackSize {
		text = text[:maxFallbackSize] + "... [truncated - parsing timed out]"
	}

	if text != "" {
		e.log.Info().
			Int("extractedLen", len(text)).
			Msg("Extracted partial text via fallback")
	}

	return text
}

// parseNestedMultipart handles nested multipart structures
func (e *Engine) parseNestedMultipart(entity *gomessage.Entity) (bodyText, bodyHTML string, hasAttachments bool) {
	mr := entity.MultipartReader()
	if mr == nil {
		return "", "", false
	}

	for {
		part, err := mr.NextPart()
		if err != nil {
			// EOF (or wrapped EOF) signals end of parts - no need to log
			break
		}

		contentType, params, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		disposition, _, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))

		if disposition == "attachment" {
			hasAttachments = true
			continue
		}

		if strings.HasPrefix(contentType, "multipart/") {
			nestedText, nestedHTML, nestedAttach := e.parseNestedMultipart(part)
			if bodyText == "" {
				bodyText = nestedText
			}
			if bodyHTML == "" {
				bodyHTML = nestedHTML
			}
			hasAttachments = hasAttachments || nestedAttach
			continue
		}

		// Read with size limit to prevent memory exhaustion
		lr := io.LimitReader(part.Body, maxPartSize)
		partBody, err := io.ReadAll(lr)
		if err != nil {
			// Check if we got partial data despite the error (e.g., malformed email missing closing boundary)
			if len(partBody) > 0 {
				e.log.Warn().
					Err(err).
					Int("partialLen", len(partBody)).
					Msg("Read partial nested part body despite error, using partial data")
				// Continue processing with partial data (don't skip)
			} else {
				continue
			}
		}

		// Log if we hit the size limit (truncated)
		if int64(len(partBody)) == maxPartSize {
			e.log.Warn().
				Int64("maxSize", maxPartSize).
				Msg("Nested part body truncated at size limit - saving partial content")
		}

		// Decode charset to UTF-8
		charset := params["charset"]
		// If no charset in header and this is HTML, try to extract from meta tags
		if charset == "" && contentType == "text/html" {
			charset = extractCharsetFromHTML(partBody)
		}
		decodedContent := decodeCharset(partBody, charset)

		switch contentType {
		case "text/plain":
			if bodyText == "" {
				bodyText = decodedContent
			}
		case "text/html":
			if bodyHTML == "" {
				bodyHTML = decodedContent
			}
		}
	}

	return bodyText, bodyHTML, hasAttachments
}

// stripHTMLTags removes HTML tags for generating snippets
func stripHTMLTags(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
		} else if !inTag {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// addressListToJSON converts an address list to JSON
func addressListToJSON(addrs []imap.Address) string {
	type addr struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	list := make([]addr, len(addrs))
	for i, a := range addrs {
		list[i] = addr{
			Name:  a.Name,
			Email: a.Addr(),
		}
	}

	data, _ := json.Marshal(list)
	return string(data)
}

// generateSnippet creates a preview snippet from message body
func generateSnippet(body string, maxLen int) string {
	// Remove excessive whitespace
	lines := strings.Split(body, "\n")
	var parts []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, ">") { // Skip quoted lines
			parts = append(parts, line)
		}
	}
	text := strings.Join(parts, " ")

	// Truncate
	if len(text) > maxLen {
		text = text[:maxLen] + "..."
	}

	return text
}

// convertFolderType converts IMAP folder type to our folder type
func convertFolderType(t imapPkg.FolderType) folder.Type {
	switch t {
	case imapPkg.FolderTypeInbox:
		return folder.TypeInbox
	case imapPkg.FolderTypeSent:
		return folder.TypeSent
	case imapPkg.FolderTypeDrafts:
		return folder.TypeDrafts
	case imapPkg.FolderTypeTrash:
		return folder.TypeTrash
	case imapPkg.FolderTypeSpam:
		return folder.TypeSpam
	case imapPkg.FolderTypeArchive:
		return folder.TypeArchive
	case imapPkg.FolderTypeAll:
		return folder.TypeAll
	case imapPkg.FolderTypeStarred:
		return folder.TypeStarred
	default:
		return folder.TypeFolder
	}
}

// extractFolderName extracts the folder name from the full path
func extractFolderName(path, delimiter string) string {
	if delimiter == "" {
		return path
	}
	parts := strings.Split(path, delimiter)
	return parts[len(parts)-1]
}

// extractReferences extracts the References header from raw message bytes
func (e *Engine) extractReferences(raw []byte) []string {
	reader := bytes.NewReader(raw)

	entity, err := gomessage.Read(reader)
	if err != nil {
		return nil
	}

	refsHeader := entity.Header.Get("References")
	if refsHeader == "" {
		return nil
	}

	// References header contains space or newline-separated Message-IDs
	// Format: <msgid1> <msgid2> <msgid3>
	var refs []string
	// Split by whitespace and filter for valid message-ids
	parts := strings.Fields(refsHeader)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "<") && strings.HasSuffix(part, ">") {
			refs = append(refs, part)
		}
	}

	return refs
}

// extractDispositionNotificationTo extracts the Disposition-Notification-To header
// This header indicates the sender is requesting a read receipt
func (e *Engine) extractDispositionNotificationTo(raw []byte) string {
	reader := bytes.NewReader(raw)

	entity, err := gomessage.Read(reader)
	if err != nil {
		return ""
	}

	dntHeader := entity.Header.Get("Disposition-Notification-To")
	if dntHeader == "" {
		return ""
	}

	// The header value is typically an email address, possibly with a name
	// e.g., "John Doe <john@example.com>" or just "john@example.com"
	return strings.TrimSpace(dntHeader)
}

// computeThreadID determines the thread ID for a message
func (e *Engine) computeThreadID(accountID string, m *message.Message) string {
	// Parse references from JSON
	var references []string
	if m.References != "" {
		json.Unmarshal([]byte(m.References), &references)
	}

	// Try to find existing thread
	threadID, err := e.messageStore.FindThreadID(accountID, m.MessageID, m.InReplyTo, references)
	if err != nil {
		e.log.Debug().Err(err).Msg("Error finding thread ID, using message ID")
		return m.MessageID
	}

	return threadID
}

// FetchRawMessage fetches the raw RFC822 content of a message from the IMAP server.
// Uses streaming fetch to avoid blocking on .Collect().
func (e *Engine) FetchRawMessage(ctx context.Context, accountID, folderID string, uid uint32) ([]byte, error) {
	// Get folder path
	f, err := e.folderStore.Get(folderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get folder: %w", err)
	}
	if f == nil {
		return nil, fmt.Errorf("folder not found: %s", folderID)
	}

	// Get a connection from the pool
	conn, err := e.pool.GetConnection(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer e.pool.Release(conn)

	// Select the mailbox
	_, err = conn.Client().SelectMailbox(ctx, f.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to select mailbox: %w", err)
	}

	// Fetch the raw message
	uidSet := imap.UIDSet{}
	uidSet.AddNum(imap.UID(uid))

	fetchOptions := &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{
			{
				Specifier: imap.PartSpecifierNone,
				Peek:      true,
			},
		},
	}

	fetchCmd := conn.Client().RawClient().Fetch(uidSet, fetchOptions)

	// Stream the single message instead of blocking on Collect()
	var rawBytes []byte

	msg := fetchCmd.Next()
	if msg == nil {
		fetchCmd.Close()
		return nil, fmt.Errorf("message not found: UID %d", uid)
	}

	// Extract body section from streamed message
	for {
		item := msg.Next()
		if item == nil {
			break
		}

		if data, ok := item.(imapclient.FetchItemDataBodySection); ok {
			if data.Literal != nil {
				lr := io.LimitReader(data.Literal, maxMessageSize)
				rawBytes, err = io.ReadAll(lr)
				if err != nil {
					fetchCmd.Close()
					return nil, fmt.Errorf("failed to read message body: %w", err)
				}
				break
			}
		}
	}

	fetchCmd.Close()

	if len(rawBytes) == 0 {
		return nil, fmt.Errorf("message body not found: UID %d", uid)
	}

	return rawBytes, nil
}

// decodeQuotedPrintableIfNeeded detects and decodes quoted-printable content if it wasn't already decoded.
// This is a safety measure for cases where go-message might not automatically decode it.
func decodeQuotedPrintableIfNeeded(content []byte) []byte {
	// Quick check: if content doesn't contain "=3D" or "=\n" patterns, it's likely not QP-encoded
	contentStr := string(content)
	if !strings.Contains(contentStr, "=3D") && !strings.Contains(contentStr, "=\n") && !strings.Contains(contentStr, "=\r\n") {
		return content
	}

	log := logging.WithComponent("quoted-printable")
	log.Debug().Msg("Detected potential quoted-printable encoding, attempting decode")

	// Try to decode as quoted-printable
	reader := quotedprintable.NewReader(bytes.NewReader(content))
	decoded, err := io.ReadAll(reader)
	if err != nil {
		log.Debug().Err(err).Msg("Quoted-printable decode failed, returning original content")
		return content
	}

	log.Debug().
		Int("originalLen", len(content)).
		Int("decodedLen", len(decoded)).
		Bool("stillHasQP", strings.Contains(string(decoded), "=3D")).
		Msg("Quoted-printable decode successful")

	return decoded
}

// decodeCharset converts content from the specified charset to UTF-8
// It handles mislabeled encodings by validating UTF-8 and auto-detecting if invalid
func decodeCharset(content []byte, declaredCharset string) string {
	log := logging.WithComponent("charset")
	log.Debug().Str("declaredCharset", declaredCharset).Int("contentLen", len(content)).Msg("Attempting charset decode")

	// If declared charset is UTF-8/ASCII or empty, validate the content
	if declaredCharset == "" || strings.EqualFold(declaredCharset, "utf-8") || strings.EqualFold(declaredCharset, "us-ascii") {
		// Check if content is actually valid UTF-8
		if utf8.Valid(content) {
			// Even if "valid UTF-8", check if it looks like misencoded text
			// by looking for high concentration of replacement chars or CJK Extension B chars
			str := string(content)
			if !looksLikeGibberish(str) {
				log.Debug().Str("declaredCharset", declaredCharset).Msg("Content is valid UTF-8")
				return str
			}
			log.Warn().Str("declaredCharset", declaredCharset).Msg("Content is valid UTF-8 but looks like gibberish, trying Chinese encodings")
		} else {
			log.Warn().Str("declaredCharset", declaredCharset).Msg("Content is NOT valid UTF-8, auto-detecting encoding")
		}

		// Try auto-detection first
		encoding, name, _ := charset.DetermineEncoding(content, "text/html")
		log.Debug().Str("detectedEncoding", name).Msg("Auto-detected encoding")

		decoded, err := encoding.NewDecoder().Bytes(content)
		if err == nil && !looksLikeGibberish(string(decoded)) {
			log.Debug().Str("detectedEncoding", name).Msg("Successfully decoded using auto-detected encoding")
			return string(decoded)
		}

		// Auto-detection failed or produced gibberish - try common Chinese encodings
		chineseEncodings := []string{"gb18030", "gbk", "gb2312", "big5", "euc-tw"}
		for _, encName := range chineseEncodings {
			enc, err := htmlindex.Get(encName)
			if err != nil {
				continue
			}
			decoded, err := enc.NewDecoder().Bytes(content)
			if err == nil && utf8.Valid(decoded) && !looksLikeGibberish(string(decoded)) {
				log.Debug().Str("triedEncoding", encName).Msg("Successfully decoded using Chinese encoding fallback")
				return string(decoded)
			}
		}

		log.Warn().Msg("All charset detection attempts failed, returning as-is")
		return string(content)
	}

	// Declared charset is something other than UTF-8 - decode it
	log.Debug().Str("declaredCharset", declaredCharset).Msg("Decoding from declared charset")

	enc, err := htmlindex.Get(declaredCharset)
	if err != nil {
		log.Warn().Err(err).Str("declaredCharset", declaredCharset).Msg("Unknown charset, trying aliases")
		// Try common aliases
		aliases := map[string]string{
			"gb2312": "gbk", // GB2312 is often actually GBK
			"x-gbk":  "gbk",
			"big5":   "big5",
			"x-big5": "big5",
		}
		if alias, ok := aliases[strings.ToLower(declaredCharset)]; ok {
			enc, err = htmlindex.Get(alias)
		}
		if err != nil {
			log.Warn().Err(err).Str("declaredCharset", declaredCharset).Msg("Unknown charset, returning as-is")
			return string(content)
		}
	}

	// Decode to UTF-8
	decoded, err := enc.NewDecoder().Bytes(content)
	if err != nil {
		log.Warn().Err(err).Str("declaredCharset", declaredCharset).Msg("Charset decoding failed, returning as-is")
		return string(content)
	}

	log.Debug().Str("declaredCharset", declaredCharset).Msg("Successfully decoded charset to UTF-8")
	return string(decoded)
}

// looksLikeGibberish checks if a string appears to be misencoded text
// by looking for telltale signs of encoding problems
func looksLikeGibberish(s string) bool {
	if len(s) == 0 {
		return false
	}

	// Count problematic characters
	var replacementCount, cjkExtBCount, total int
	for _, r := range s {
		total++
		if r == '\ufffd' { // Unicode replacement character
			replacementCount++
		}
		// CJK Extension B range (U+20000-U+2A6DF) often indicates misencoding
		// These are rare characters that shouldn't appear frequently in normal text
		if r >= 0x20000 && r <= 0x2A6DF {
			cjkExtBCount++
		}
	}

	// If more than 10% replacement characters, it's gibberish
	if total > 10 && float64(replacementCount)/float64(total) > 0.1 {
		return true
	}

	// If more than 5% CJK Extension B characters, likely misencoded
	// (these are extremely rare in normal Chinese text)
	if total > 20 && float64(cjkExtBCount)/float64(total) > 0.05 {
		return true
	}

	return false
}

// extractCharsetFromHTML extracts charset from HTML meta tags
// This is used as a fallback when Content-Type header doesn't specify charset
func extractCharsetFromHTML(html []byte) string {
	log := logging.WithComponent("charset")

	// Only check the first 1024 bytes for performance (meta tags are always near the top)
	searchBytes := html
	if len(html) > 1024 {
		searchBytes = html[:1024]
	}

	// Log the first 200 bytes for debugging (to see what meta tags are present)
	preview := searchBytes
	if len(preview) > 200 {
		preview = preview[:200]
	}
	log.Debug().
		Str("htmlPreview", string(preview)).
		Int("searchLen", len(searchBytes)).
		Msg("Searching for charset in HTML")

	// Pattern 1: <meta charset="...">
	re1 := regexp.MustCompile(`(?i)<meta[^>]+charset=["']?([^"'\s>]+)`)
	if match := re1.FindSubmatch(searchBytes); len(match) > 1 {
		charset := string(match[1])
		log.Debug().Str("charset", charset).Msg("Found charset via meta charset attribute")
		return charset
	}

	// Pattern 2: <meta http-equiv="Content-Type" content="text/html; charset=...">
	re2 := regexp.MustCompile(`(?i)<meta[^>]+content=["'][^"']*charset=([^"'\s;]+)`)
	if match := re2.FindSubmatch(searchBytes); len(match) > 1 {
		charset := string(match[1])
		log.Debug().Str("charset", charset).Msg("Found charset via meta http-equiv")
		return charset
	}

	log.Debug().Msg("No charset found in HTML meta tags")
	return ""
}
