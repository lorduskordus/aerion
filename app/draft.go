package app

import (
	"fmt"
	"time"

	goImap "github.com/emersion/go-imap/v2"
	"github.com/hkdb/aerion/internal/draft"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/smtp"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// DraftResult represents the result of saving a draft
type DraftResult struct {
	Draft *draft.Draft `json:"draft"`
}

// ============================================================================
// Draft API - Exposed to frontend via Wails bindings
// ============================================================================

// SaveDraft saves or updates a draft email to the local database and syncs to IMAP.
// If existingDraftID is provided and exists, updates that draft; otherwise creates a new one.
func (a *App) SaveDraft(accountID string, msg smtp.ComposeMessage, existingDraftID string) (*DraftResult, error) {
	log := logging.WithComponent("app")

	log.Debug().
		Str("accountID", accountID).
		Str("existingDraftID", existingDraftID).
		Str("subject", msg.Subject).
		Msg("SaveDraft called")

	var localDraft *draft.Draft

	// Try to load existing draft if ID provided
	if existingDraftID != "" {
		existing, err := a.draftStore.Get(existingDraftID)
		if err != nil {
			log.Warn().Err(err).Str("draftID", existingDraftID).Msg("Failed to load existing draft")
		} else if existing != nil {
			localDraft = existing
			log.Debug().Str("draftID", existingDraftID).Msg("Loaded existing draft for update")
		}
	}

	if localDraft != nil {
		// Update existing draft
		localDraft.ToList = addressListToJSON(msg.To)
		localDraft.CcList = addressListToJSON(msg.Cc)
		localDraft.BccList = addressListToJSON(msg.Bcc)
		localDraft.Subject = msg.Subject
		localDraft.BodyHTML = msg.HTMLBody
		localDraft.BodyText = msg.TextBody
		localDraft.InReplyToID = msg.InReplyTo
		localDraft.SyncStatus = draft.SyncStatusPending

		if err := a.draftStore.Update(localDraft); err != nil {
			return nil, fmt.Errorf("failed to update draft: %w", err)
		}
		log.Debug().Str("draftID", localDraft.ID).Msg("Updated existing draft")
	} else {
		// Create new draft
		localDraft = &draft.Draft{
			AccountID:   accountID,
			ToList:      addressListToJSON(msg.To),
			CcList:      addressListToJSON(msg.Cc),
			BccList:     addressListToJSON(msg.Bcc),
			Subject:     msg.Subject,
			BodyHTML:    msg.HTMLBody,
			BodyText:    msg.TextBody,
			InReplyToID: msg.InReplyTo,
			SyncStatus:  draft.SyncStatusPending,
		}

		if err := a.draftStore.Create(localDraft); err != nil {
			return nil, fmt.Errorf("failed to create draft: %w", err)
		}
		log.Debug().Str("draftID", localDraft.ID).Msg("Created new draft")
	}

	// Sync to IMAP in background
	go a.syncDraftToIMAP(localDraft, msg)

	log.Info().Str("draftID", localDraft.ID).Msg("Draft saved locally, syncing to IMAP")
	return &DraftResult{Draft: localDraft}, nil
}

// syncDraftToIMAP syncs a draft to the IMAP server
func (a *App) syncDraftToIMAP(localDraft *draft.Draft, msg smtp.ComposeMessage) {
	log := logging.WithComponent("app")

	// Find the Drafts folder for this account
	draftsFolder, err := a.GetSpecialFolder(localDraft.AccountID, folder.TypeDrafts)
	if err != nil || draftsFolder == nil {
		log.Warn().Err(err).Str("account_id", localDraft.AccountID).Msg("No drafts folder found, skipping IMAP sync")
		a.draftStore.UpdateSyncStatus(localDraft.ID, draft.SyncStatusFailed, 0, "", "no drafts folder found")
		return
	}

	// Get IMAP connection from pool
	poolConn, err := a.imapPool.GetConnection(a.ctx, localDraft.AccountID)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get IMAP connection, will retry later")
		a.draftStore.UpdateSyncStatus(localDraft.ID, draft.SyncStatusFailed, 0, "", err.Error())
		return
	}
	defer a.imapPool.Release(poolConn)

	conn := poolConn.Client()

	// Delete old IMAP draft if it exists
	if localDraft.IMAPUID > 0 && localDraft.FolderID != "" {
		if _, err := conn.SelectMailbox(a.ctx, draftsFolder.Path); err == nil {
			if err := conn.DeleteMessageByUID(goImap.UID(localDraft.IMAPUID)); err != nil {
				log.Warn().Err(err).Uint32("uid", localDraft.IMAPUID).Msg("Failed to delete old draft from IMAP")
			}
		}
	}

	// Build RFC822 message
	rawMsg, err := msg.ToRFC822()
	if err != nil {
		log.Error().Err(err).Msg("Failed to build RFC822 message")
		a.draftStore.UpdateSyncStatus(localDraft.ID, draft.SyncStatusFailed, 0, "", err.Error())
		return
	}

	// Append to IMAP Drafts folder with \Draft and \Seen flags
	flags := []goImap.Flag{goImap.FlagDraft, goImap.FlagSeen}
	uid, err := conn.AppendMessage(draftsFolder.Path, flags, time.Now(), rawMsg)
	if err != nil {
		log.Error().Err(err).Msg("Failed to append draft to IMAP")
		a.draftStore.UpdateSyncStatus(localDraft.ID, draft.SyncStatusFailed, 0, "", err.Error())
		return
	}

	// Update local draft with sync status
	if err := a.draftStore.UpdateSyncStatus(localDraft.ID, draft.SyncStatusSynced, uint32(uid), draftsFolder.ID, ""); err != nil {
		log.Warn().Err(err).Msg("Failed to update draft sync status")
	}

	log.Info().
		Str("id", localDraft.ID).
		Uint32("imap_uid", uint32(uid)).
		Msg("Draft synced to IMAP")

	// Sync the Drafts folder so the main window's message list shows the updated draft
	// Do this after IMAP upload completes to ensure the draft is available
	if err := a.SyncFolder(localDraft.AccountID, draftsFolder.ID); err != nil {
		log.Warn().Err(err).Str("folderID", draftsFolder.ID).Msg("Failed to sync Drafts folder after draft save")
	} else {
		log.Debug().Str("folderID", draftsFolder.ID).Msg("Synced Drafts folder after draft save")
	}
}

// SyncPendingDrafts syncs any pending drafts for an account
func (a *App) SyncPendingDrafts(accountID string) error {
	log := logging.WithComponent("app")

	pending, err := a.draftStore.ListPendingSync(accountID)
	if err != nil {
		return fmt.Errorf("failed to list pending drafts: %w", err)
	}

	if len(pending) == 0 {
		return nil
	}

	log.Info().Int("count", len(pending)).Str("accountID", accountID).Msg("Syncing pending drafts")

	for _, d := range pending {
		msg := a.draftToComposeMessage(d)
		a.syncDraftToIMAP(d, *msg)
	}

	return nil
}

// syncAllPendingDrafts syncs pending drafts for all accounts
func (a *App) syncAllPendingDrafts() {
	log := logging.WithComponent("app")

	accounts, err := a.accountStore.List()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to list accounts for draft sync")
		return
	}

	for _, acc := range accounts {
		if !acc.Enabled {
			continue
		}
		if err := a.SyncPendingDrafts(acc.ID); err != nil {
			log.Warn().Err(err).Str("accountID", acc.ID).Msg("Failed to sync pending drafts")
		}
	}
}

// draftToComposeMessage converts a draft to a ComposeMessage
func (a *App) draftToComposeMessage(d *draft.Draft) *smtp.ComposeMessage {
	return &smtp.ComposeMessage{
		To:        parseAddressList(d.ToList),
		Cc:        parseAddressList(d.CcList),
		Bcc:       parseAddressList(d.BccList),
		Subject:   d.Subject,
		HTMLBody:  d.BodyHTML,
		TextBody:  d.BodyText,
		InReplyTo: d.InReplyToID,
	}
}

// DeleteDraft deletes a draft from local DB and IMAP
func (a *App) DeleteDraft(draftID string) error {
	log := logging.WithComponent("app")

	// Get the draft to find IMAP UID
	d, err := a.draftStore.Get(draftID)
	if err != nil {
		return fmt.Errorf("failed to get draft: %w", err)
	}
	if d == nil {
		return nil // Already deleted
	}

	// Delete from IMAP if synced
	if d.IsSynced() {
		draftsFolder, _ := a.GetSpecialFolder(d.AccountID, folder.TypeDrafts)
		if draftsFolder != nil {
			poolConn, err := a.imapPool.GetConnection(a.ctx, d.AccountID)
			if err == nil {
				defer a.imapPool.Release(poolConn)
				conn := poolConn.Client()
				if _, err := conn.SelectMailbox(a.ctx, draftsFolder.Path); err == nil {
					if err := conn.DeleteMessageByUID(goImap.UID(d.IMAPUID)); err != nil {
						log.Warn().Err(err).Uint32("uid", d.IMAPUID).Msg("Failed to delete draft from IMAP")
					}
				}
			}
		}
	}

	// Delete from local database
	if err := a.draftStore.Delete(draftID); err != nil {
		return fmt.Errorf("failed to delete draft: %w", err)
	}

	// Emit event
	wailsRuntime.EventsEmit(a.ctx, "draft:deleted", map[string]interface{}{
		"draftId": draftID,
	})

	log.Info().Str("draftID", draftID).Msg("Draft deleted")
	return nil
}

// GetDraft returns a draft by ID as a ComposeMessage (for editing in composer)
// The ID can be either a draft ID or a message ID (from the Drafts folder)
func (a *App) GetDraft(id string) (*smtp.ComposeMessage, error) {
	log := logging.WithComponent("app")

	// First, try to get it as a draft ID
	d, err := a.draftStore.Get(id)
	if err != nil {
		return nil, err
	}
	if d != nil {
		log.Debug().Str("draftID", id).Msg("Found draft by draft ID")
		return a.draftToComposeMessage(d), nil
	}

	// Not found as draft ID - try as message ID
	// Get the message to find its IMAP UID and folder
	msg, err := a.messageStore.Get(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return nil, nil
	}

	// Look up draft by IMAP UID and folder
	d, err = a.draftStore.GetByIMAPUID(msg.FolderID, msg.UID)
	if err != nil {
		return nil, err
	}
	if d != nil {
		log.Debug().Str("messageID", id).Str("draftID", d.ID).Msg("Found draft by message IMAP UID")
		return a.draftToComposeMessage(d), nil
	}

	// No draft found - this might be a draft that was created outside Aerion
	// (e.g., from webmail). Build a ComposeMessage from the message itself.
	log.Debug().Str("messageID", id).Msg("No local draft found, building from message")
	return a.messageToComposeMessage(msg), nil
}

// messageToComposeMessage converts a message (from Drafts folder) to a ComposeMessage
func (a *App) messageToComposeMessage(msg *message.Message) *smtp.ComposeMessage {
	return &smtp.ComposeMessage{
		To:        parseAddressList(msg.ToList),
		Cc:        parseAddressList(msg.CcList),
		Bcc:       parseAddressList(msg.BccList),
		Subject:   msg.Subject,
		HTMLBody:  msg.BodyHTML,
		TextBody:  msg.BodyText,
		InReplyTo: msg.InReplyTo,
	}
}

// ListDrafts returns all drafts for an account
func (a *App) ListDrafts(accountID string) ([]*draft.Draft, error) {
	return a.draftStore.ListByAccount(accountID)
}
