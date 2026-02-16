package app

import (
	"context"
	"fmt"
	"time"

	goImap "github.com/emersion/go-imap/v2"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/imap"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/undo"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// withIMAPRetry wraps an IMAP operation with stale-connection retry.
// If the operation fails with a connection error, the dead connection is discarded
// and the operation is retried once with a fresh connection.
func (a *App) withIMAPRetry(accountID string, op func(conn *imap.Client) error) error {
	log := logging.WithComponent("app.imapRetry")

	poolConn, err := a.imapPool.GetConnection(a.ctx, accountID)
	if err != nil {
		return fmt.Errorf("failed to get IMAP connection: %w", err)
	}

	err = op(poolConn.Client())
	if err == nil {
		a.imapPool.Release(poolConn)
		return nil
	}

	if !imap.IsConnectionError(err) {
		a.imapPool.Release(poolConn)
		return err
	}

	// Stale connection — discard and retry once with fresh connection
	log.Warn().Err(err).Str("account", accountID).Msg("IMAP connection error, retrying with fresh connection")
	a.imapPool.Discard(poolConn)

	poolConn, err = a.imapPool.GetConnection(a.ctx, accountID)
	if err != nil {
		return fmt.Errorf("failed to get IMAP connection on retry: %w", err)
	}
	defer a.imapPool.Release(poolConn)

	return op(poolConn.Client())
}

// ============================================================================
// Message Actions API - Exposed to frontend via Wails bindings
// ============================================================================

// MarkAsRead marks messages as read
func (a *App) MarkAsRead(messageIDs []string) error {
	return a.setReadStatus(messageIDs, true)
}

// MarkAsUnread marks messages as unread
func (a *App) MarkAsUnread(messageIDs []string) error {
	return a.setReadStatus(messageIDs, false)
}

func (a *App) setReadStatus(messageIDs []string, isRead bool) error {
	log := logging.WithComponent("app")

	if len(messageIDs) == 0 {
		return nil
	}

	// Get messages to find their UIDs and folders
	messages, err := a.messageStore.GetByIDs(messageIDs)
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}
	if len(messages) == 0 {
		return nil
	}

	// Group by folder for IMAP operations
	byFolder := make(map[string][]*message.Message)
	for _, m := range messages {
		byFolder[m.FolderID] = append(byFolder[m.FolderID], m)
	}

	// Update local DB first (local-first)
	isReadPtr := &isRead
	if err := a.messageStore.UpdateFlagsBatch(messageIDs, isReadPtr, nil); err != nil {
		return fmt.Errorf("failed to update local flags: %w", err)
	}

	// Emit event for UI update with flag state
	wailsRuntime.EventsEmit(a.ctx, "messages:flagsChanged", map[string]interface{}{
		"messageIds": messageIDs,
		"isRead":     isRead,
	})

	// Update folder unread counts in background to avoid blocking other DB operations
	go func() {
		folderCounts := make(map[string]int)
		for folderID := range byFolder {
			unreadCount, err := a.messageStore.CountUnreadByFolder(folderID)
			if err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to count unread messages")
				continue
			}
			folderObj, err := a.folderStore.Get(folderID)
			if err != nil || folderObj == nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to get folder")
				continue
			}
			if err := a.folderStore.UpdateCounts(folderID, folderObj.TotalCount, unreadCount); err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to update folder counts")
				continue
			}
			folderCounts[folderID] = unreadCount
		}
		if len(folderCounts) > 0 {
			wailsRuntime.EventsEmit(a.ctx, "folders:countsChanged", folderCounts)
		}
	}()

	// Sync to IMAP in background with retry
	go func() {
		for folderID, msgs := range byFolder {
			var err error
			for attempt := 1; attempt <= 3; attempt++ {
				err = a.syncFlagsToIMAP(msgs, folderID, "read", isRead)
				if err == nil {
					break
				}
				log.Warn().Err(err).Int("attempt", attempt).Str("folderID", folderID).Msg("Failed to sync read flags to IMAP, retrying...")
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			if err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to sync read flags to IMAP after 3 attempts")
			}
		}
	}()

	// Create undo command
	firstMsg := messages[0]
	folderObj, _ := a.folderStore.Get(firstMsg.FolderID)
	if folderObj != nil {
		uids := make([]uint32, len(messages))
		for i, m := range messages {
			uids[i] = m.UID
		}

		description := "Mark as read"
		if !isRead {
			description = "Mark as unread"
		}

		cmd := undo.NewFlagChangeCommand(
			a.ctx,
			a,
			firstMsg.AccountID,
			folderObj.Path,
			messageIDs,
			uids,
			"read",
			!isRead, // previous state was opposite
			description,
		)
		a.undoStack.Push(cmd)
	}

	return nil
}

// Star marks messages as starred
func (a *App) Star(messageIDs []string) error {
	return a.setStarredStatus(messageIDs, true)
}

// Unstar removes star from messages
func (a *App) Unstar(messageIDs []string) error {
	return a.setStarredStatus(messageIDs, false)
}

func (a *App) setStarredStatus(messageIDs []string, isStarred bool) error {
	log := logging.WithComponent("app")

	if len(messageIDs) == 0 {
		return nil
	}

	messages, err := a.messageStore.GetByIDs(messageIDs)
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}
	if len(messages) == 0 {
		return nil
	}

	byFolder := make(map[string][]*message.Message)
	for _, m := range messages {
		byFolder[m.FolderID] = append(byFolder[m.FolderID], m)
	}

	// Update local DB first
	isStarredPtr := &isStarred
	if err := a.messageStore.UpdateFlagsBatch(messageIDs, nil, isStarredPtr); err != nil {
		return fmt.Errorf("failed to update local flags: %w", err)
	}

	wailsRuntime.EventsEmit(a.ctx, "messages:flagsChanged", messageIDs)

	// Sync to IMAP in background with retry
	go func() {
		for folderID, msgs := range byFolder {
			var err error
			for attempt := 1; attempt <= 3; attempt++ {
				err = a.syncFlagsToIMAP(msgs, folderID, "starred", isStarred)
				if err == nil {
					break
				}
				log.Warn().Err(err).Int("attempt", attempt).Str("folderID", folderID).Msg("Failed to sync starred flags to IMAP, retrying...")
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			if err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to sync starred flags to IMAP after 3 attempts")
			}
		}
	}()

	// Create undo command
	firstMsg := messages[0]
	folderObj, _ := a.folderStore.Get(firstMsg.FolderID)
	if folderObj != nil {
		uids := make([]uint32, len(messages))
		for i, m := range messages {
			uids[i] = m.UID
		}

		description := "Star"
		if !isStarred {
			description = "Unstar"
		}

		cmd := undo.NewFlagChangeCommand(
			a.ctx,
			a,
			firstMsg.AccountID,
			folderObj.Path,
			messageIDs,
			uids,
			"starred",
			!isStarred,
			description,
		)
		a.undoStack.Push(cmd)
	}

	return nil
}

// syncFlagsToIMAP syncs flag changes to IMAP server
func (a *App) syncFlagsToIMAP(messages []*message.Message, folderID, flagType string, flagValue bool) error {
	if len(messages) == 0 {
		return nil
	}

	folderObj, err := a.folderStore.Get(folderID)
	if err != nil || folderObj == nil {
		return fmt.Errorf("folder not found: %s", folderID)
	}

	uids := make([]goImap.UID, len(messages))
	for i, m := range messages {
		uids[i] = goImap.UID(m.UID)
	}

	var flag goImap.Flag
	switch flagType {
	case "read":
		flag = goImap.FlagSeen
	case "starred":
		flag = goImap.FlagFlagged
	}

	return a.withIMAPRetry(messages[0].AccountID, func(conn *imap.Client) error {
		if _, err := conn.SelectMailbox(a.ctx, folderObj.Path); err != nil {
			return fmt.Errorf("failed to select mailbox: %w", err)
		}

		if flagValue {
			return conn.AddMessageFlags(uids, []goImap.Flag{flag})
		}
		return conn.RemoveMessageFlags(uids, []goImap.Flag{flag})
	})
}

// MoveToFolder moves messages to a specified folder
func (a *App) MoveToFolder(messageIDs []string, destFolderID string) error {
	log := logging.WithComponent("app")

	if len(messageIDs) == 0 {
		return nil
	}

	messages, err := a.messageStore.GetByIDs(messageIDs)
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}
	if len(messages) == 0 {
		return nil
	}

	destFolder, err := a.folderStore.Get(destFolderID)
	if err != nil || destFolder == nil {
		return fmt.Errorf("destination folder not found: %s", destFolderID)
	}

	// Group by source folder
	byFolder := make(map[string][]*message.Message)
	for _, m := range messages {
		byFolder[m.FolderID] = append(byFolder[m.FolderID], m)
	}

	// Update local DB first
	if err := a.messageStore.MoveMessages(messageIDs, destFolderID); err != nil {
		return fmt.Errorf("failed to move messages locally: %w", err)
	}

	wailsRuntime.EventsEmit(a.ctx, "messages:moved", map[string]interface{}{
		"messageIds":   messageIDs,
		"destFolderId": destFolderID,
	})

	// Update folder unread counts for source and destination folders
	go func() {
		folderCounts := make(map[string]int)

		// Update source folders
		for folderID, msgs := range byFolder {
			unreadCount, err := a.messageStore.CountUnreadByFolder(folderID)
			if err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to count unread messages")
				continue
			}
			folderObj, err := a.folderStore.Get(folderID)
			if err != nil || folderObj == nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to get folder")
				continue
			}
			newTotalCount := folderObj.TotalCount - len(msgs)
			if newTotalCount < 0 {
				newTotalCount = 0
			}
			if err := a.folderStore.UpdateCounts(folderID, newTotalCount, unreadCount); err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to update folder counts")
				continue
			}
			folderCounts[folderID] = unreadCount
		}

		// Update destination folder
		unreadCount, err := a.messageStore.CountUnreadByFolder(destFolderID)
		if err != nil {
			log.Error().Err(err).Str("folderID", destFolderID).Msg("Failed to count unread messages for destination")
		} else {
			destFolderObj, err := a.folderStore.Get(destFolderID)
			if err == nil && destFolderObj != nil {
				newTotalCount := destFolderObj.TotalCount + len(messageIDs)
				if err := a.folderStore.UpdateCounts(destFolderID, newTotalCount, unreadCount); err != nil {
					log.Error().Err(err).Str("folderID", destFolderID).Msg("Failed to update destination folder counts")
				} else {
					folderCounts[destFolderID] = unreadCount
				}
			}
		}

		if len(folderCounts) > 0 {
			wailsRuntime.EventsEmit(a.ctx, "folders:countsChanged", folderCounts)
		}
	}()

	// Sync to IMAP in background (COPY + DELETE), then sync destination to get correct UIDs.
	// Use a timeout context so this goroutine doesn't persist through sleep/wake cycles
	// holding pool connections indefinitely.
	go func() {
		moveCtx, moveCancel := context.WithTimeout(a.ctx, 5*time.Minute)
		defer moveCancel()

		for sourceFolderID, msgs := range byFolder {
			if err := a.moveMessagesToIMAP(msgs, sourceFolderID, destFolder); err != nil {
				log.Error().Err(err).
					Str("sourceFolderID", sourceFolderID).
					Str("destFolderID", destFolderID).
					Msg("Failed to move messages on IMAP")
				return
			}
		}

		// Sync destination folder so moved messages get correct UIDs (headers + bodies)
		if len(messages) > 0 {
			syncPeriodDays := 30
			if acc, err := a.accountStore.Get(messages[0].AccountID); err == nil && acc != nil {
				syncPeriodDays = acc.SyncPeriodDays
			}
			if err := a.syncEngine.SyncMessages(moveCtx, messages[0].AccountID, destFolderID, syncPeriodDays); err != nil {
				log.Warn().Err(err).Str("destFolderID", destFolderID).Msg("Failed to sync destination folder after move")
			}
			if err := a.syncEngine.FetchBodiesInBackground(moveCtx, messages[0].AccountID, destFolderID, syncPeriodDays); err != nil {
				log.Warn().Err(err).Str("destFolderID", destFolderID).Msg("Failed to fetch bodies for destination folder after move")
			}
			wailsRuntime.EventsEmit(a.ctx, "folder:synced", map[string]interface{}{
				"accountId": messages[0].AccountID,
				"folderId":  destFolderID,
			})
		}
	}()

	// Create undo command for each source folder
	for sourceFolderID, msgs := range byFolder {
		sourceFolder, _ := a.folderStore.Get(sourceFolderID)
		if sourceFolder == nil {
			continue
		}

		msgIDs := make([]string, len(msgs))
		uids := make([]uint32, len(msgs))
		for i, m := range msgs {
			msgIDs[i] = m.ID
			uids[i] = m.UID
		}

		cmd := undo.NewMoveCommand(
			a.ctx,
			a,
			msgs[0].AccountID,
			msgIDs,
			uids,
			sourceFolderID,
			sourceFolder.Path,
			destFolderID,
			destFolder.Path,
			fmt.Sprintf("Move to %s", destFolder.Name),
		)
		a.undoStack.Push(cmd)
	}

	return nil
}

func (a *App) moveMessagesToIMAP(messages []*message.Message, sourceFolderID string, destFolder *folder.Folder) error {
	log := logging.WithComponent("app.moveMessagesToIMAP")

	if len(messages) == 0 {
		return nil
	}

	sourceFolder, err := a.folderStore.Get(sourceFolderID)
	if err != nil || sourceFolder == nil {
		return fmt.Errorf("source folder not found")
	}

	// Collect UIDs for logging
	uidList := make([]uint32, len(messages))
	for i, m := range messages {
		uidList[i] = m.UID
	}

	log.Info().
		Str("sourceFolder", sourceFolder.Path).
		Str("destFolder", destFolder.Path).
		Uints32("uids", uidList).
		Int("count", len(messages)).
		Msg("Starting IMAP move operation")

	uids := make([]goImap.UID, len(messages))
	for i, m := range messages {
		uids[i] = goImap.UID(m.UID)
	}

	err = a.withIMAPRetry(messages[0].AccountID, func(conn *imap.Client) error {
		// Select source mailbox
		log.Debug().Str("mailbox", sourceFolder.Path).Msg("Selecting source mailbox")
		if _, err := conn.SelectMailbox(a.ctx, sourceFolder.Path); err != nil {
			return fmt.Errorf("failed to select source mailbox: %w", err)
		}

		// COPY to destination
		log.Debug().Str("destMailbox", destFolder.Path).Msg("Copying messages to destination")
		if _, err := conn.CopyMessages(uids, destFolder.Path); err != nil {
			return fmt.Errorf("failed to copy messages: %w", err)
		}
		log.Debug().Msg("Messages copied successfully")

		// DELETE from source
		log.Debug().Msg("Deleting messages from source (marking deleted + expunge)")
		if err := conn.DeleteMessagesByUID(uids); err != nil {
			return fmt.Errorf("failed to delete messages from source: %w", err)
		}

		return nil
	})

	if err != nil {
		log.Error().Err(err).Msg("IMAP move operation failed")
		return err
	}

	log.Info().
		Str("sourceFolder", sourceFolder.Path).
		Str("destFolder", destFolder.Path).
		Int("count", len(messages)).
		Msg("IMAP move operation completed successfully")

	return nil
}

// CopyToFolder copies messages to a specified folder (keeps original)
// Unlike MoveToFolder, this only copies - original messages remain in place
func (a *App) CopyToFolder(messageIDs []string, destFolderID string) error {
	log := logging.WithComponent("app")

	if len(messageIDs) == 0 {
		return nil
	}

	messages, err := a.messageStore.GetByIDs(messageIDs)
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}
	if len(messages) == 0 {
		return nil
	}

	destFolder, err := a.folderStore.Get(destFolderID)
	if err != nil || destFolder == nil {
		return fmt.Errorf("destination folder not found: %s", destFolderID)
	}

	// Group by source folder
	byFolder := make(map[string][]*message.Message)
	for _, m := range messages {
		byFolder[m.FolderID] = append(byFolder[m.FolderID], m)
	}

	// Copy on IMAP (no local DB change - messages stay in source folder)
	go func() {
		for sourceFolderID, msgs := range byFolder {
			if err := a.copyMessagesToIMAP(msgs, sourceFolderID, destFolder); err != nil {
				log.Error().Err(err).
					Str("sourceFolderID", sourceFolderID).
					Str("destFolderID", destFolderID).
					Msg("Failed to copy messages on IMAP")
			}
		}

		// After copy completes, sync destination folder to fetch new copies
		if len(messages) > 0 {
			// Get account sync period (0 means all messages)
			syncPeriodDays := 30
			if acc, err := a.accountStore.Get(messages[0].AccountID); err == nil && acc != nil {
				syncPeriodDays = acc.SyncPeriodDays
			}
			if err := a.syncEngine.SyncMessages(a.ctx, messages[0].AccountID, destFolderID, syncPeriodDays); err != nil {
				log.Warn().Err(err).Str("destFolderID", destFolderID).Msg("Failed to sync destination folder after copy")
			}
		}

		// Emit event after sync completes
		wailsRuntime.EventsEmit(a.ctx, "messages:copied", map[string]interface{}{
			"messageIds":   messageIDs,
			"destFolderId": destFolderID,
		})
	}()

	return nil
}

func (a *App) copyMessagesToIMAP(messages []*message.Message, sourceFolderID string, destFolder *folder.Folder) error {
	if len(messages) == 0 {
		return nil
	}

	sourceFolder, err := a.folderStore.Get(sourceFolderID)
	if err != nil || sourceFolder == nil {
		return fmt.Errorf("source folder not found")
	}

	uids := make([]goImap.UID, len(messages))
	for i, m := range messages {
		uids[i] = goImap.UID(m.UID)
	}

	return a.withIMAPRetry(messages[0].AccountID, func(conn *imap.Client) error {
		if _, err := conn.SelectMailbox(a.ctx, sourceFolder.Path); err != nil {
			return fmt.Errorf("failed to select source mailbox: %w", err)
		}

		// COPY to destination (no DELETE - messages stay in source)
		if _, err := conn.CopyMessages(uids, destFolder.Path); err != nil {
			return fmt.Errorf("failed to copy messages: %w", err)
		}

		return nil
	})
}

// Archive moves messages to the Archive folder
func (a *App) Archive(messageIDs []string) error {
	if len(messageIDs) == 0 {
		return nil
	}

	// Get first message to determine account
	messages, err := a.messageStore.GetByIDs(messageIDs[:1])
	if err != nil || len(messages) == 0 {
		return fmt.Errorf("failed to get message")
	}

	archiveFolder, err := a.GetSpecialFolder(messages[0].AccountID, folder.TypeArchive)
	if err != nil {
		return fmt.Errorf("failed to get archive folder: %w", err)
	}
	if archiveFolder == nil {
		return fmt.Errorf("no archive folder configured")
	}

	return a.MoveToFolder(messageIDs, archiveFolder.ID)
}

// Trash moves messages to the Trash folder
func (a *App) Trash(messageIDs []string) error {
	if len(messageIDs) == 0 {
		return nil
	}

	messages, err := a.messageStore.GetByIDs(messageIDs[:1])
	if err != nil || len(messages) == 0 {
		return fmt.Errorf("failed to get message")
	}

	trashFolder, err := a.GetSpecialFolder(messages[0].AccountID, folder.TypeTrash)
	if err != nil {
		return fmt.Errorf("failed to get trash folder: %w", err)
	}
	if trashFolder == nil {
		return fmt.Errorf("no trash folder configured")
	}

	return a.MoveToFolder(messageIDs, trashFolder.ID)
}

// MarkAsSpam moves messages to the Spam folder
func (a *App) MarkAsSpam(messageIDs []string) error {
	if len(messageIDs) == 0 {
		return nil
	}

	messages, err := a.messageStore.GetByIDs(messageIDs[:1])
	if err != nil || len(messages) == 0 {
		return fmt.Errorf("failed to get message")
	}

	spamFolder, err := a.GetSpecialFolder(messages[0].AccountID, folder.TypeSpam)
	if err != nil {
		return fmt.Errorf("failed to get spam folder: %w", err)
	}
	if spamFolder == nil {
		return fmt.Errorf("no spam folder configured")
	}

	return a.MoveToFolder(messageIDs, spamFolder.ID)
}

// MarkAsNotSpam moves messages from Spam to Inbox
func (a *App) MarkAsNotSpam(messageIDs []string) error {
	if len(messageIDs) == 0 {
		return nil
	}

	messages, err := a.messageStore.GetByIDs(messageIDs[:1])
	if err != nil || len(messages) == 0 {
		return fmt.Errorf("failed to get message")
	}

	inboxFolder, err := a.folderStore.GetByType(messages[0].AccountID, folder.TypeInbox)
	if err != nil {
		return fmt.Errorf("failed to get inbox folder: %w", err)
	}
	if inboxFolder == nil {
		return fmt.Errorf("no inbox folder found")
	}

	return a.MoveToFolder(messageIDs, inboxFolder.ID)
}

// DeletePermanently permanently deletes messages
func (a *App) DeletePermanently(messageIDs []string) error {
	log := logging.WithComponent("app")

	if len(messageIDs) == 0 {
		return nil
	}

	messages, err := a.messageStore.GetByIDs(messageIDs)
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}
	if len(messages) == 0 {
		return nil
	}

	// Group by folder
	byFolder := make(map[string][]*message.Message)
	for _, m := range messages {
		byFolder[m.FolderID] = append(byFolder[m.FolderID], m)
	}

	// Delete from local DB first
	if err := a.messageStore.DeleteBatch(messageIDs); err != nil {
		return fmt.Errorf("failed to delete messages locally: %w", err)
	}

	wailsRuntime.EventsEmit(a.ctx, "messages:deleted", messageIDs)

	// Update folder unread counts
	go func() {
		folderCounts := make(map[string]int)
		for folderID, msgs := range byFolder {
			unreadCount, err := a.messageStore.CountUnreadByFolder(folderID)
			if err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to count unread messages")
				continue
			}
			folderObj, err := a.folderStore.Get(folderID)
			if err != nil || folderObj == nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to get folder")
				continue
			}
			newTotalCount := folderObj.TotalCount - len(msgs)
			if newTotalCount < 0 {
				newTotalCount = 0
			}
			if err := a.folderStore.UpdateCounts(folderID, newTotalCount, unreadCount); err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to update folder counts")
				continue
			}
			folderCounts[folderID] = unreadCount
		}
		if len(folderCounts) > 0 {
			wailsRuntime.EventsEmit(a.ctx, "folders:countsChanged", folderCounts)
		}
	}()

	// Delete from IMAP in background
	go func() {
		for folderID, msgs := range byFolder {
			if err := a.deleteMessagesFromIMAP(msgs, folderID); err != nil {
				log.Error().Err(err).Str("folderID", folderID).Msg("Failed to delete messages from IMAP")
			}
		}
	}()

	// Note: Permanent delete undo is complex - would need to store full message content
	// For now, we don't add to undo stack for permanent deletes

	return nil
}

func (a *App) deleteMessagesFromIMAP(messages []*message.Message, folderID string) error {
	if len(messages) == 0 {
		return nil
	}

	folderObj, err := a.folderStore.Get(folderID)
	if err != nil || folderObj == nil {
		return fmt.Errorf("folder not found")
	}

	uids := make([]goImap.UID, len(messages))
	for i, m := range messages {
		uids[i] = goImap.UID(m.UID)
	}

	return a.withIMAPRetry(messages[0].AccountID, func(conn *imap.Client) error {
		if _, err := conn.SelectMailbox(a.ctx, folderObj.Path); err != nil {
			return fmt.Errorf("failed to select mailbox: %w", err)
		}

		return conn.DeleteMessagesByUID(uids)
	})
}
