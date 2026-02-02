package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	goImap "github.com/emersion/go-imap/v2"
	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/contact"
	"github.com/hkdb/aerion/internal/credentials"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/draft"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/imap"
	"github.com/hkdb/aerion/internal/ipc"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/oauth2"
	"github.com/hkdb/aerion/internal/platform"
	"github.com/hkdb/aerion/internal/settings"
	"github.com/hkdb/aerion/internal/smtp"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ComposerConfig holds the configuration for a composer window.
type ComposerConfig struct {
	AccountID  string // Required: account to compose from
	IPCAddress string // Required: address of main window's IPC server
	Mode       string // "new", "reply", "reply-all", "forward"
	MessageID  string // Original message ID (for reply/forward)
	DraftID    string // Draft ID to resume editing
}

// ComposeMode represents the compose mode data returned to the frontend.
type ComposeMode struct {
	AccountID string `json:"accountId"`
	Mode      string `json:"mode"`
	MessageID string `json:"messageId"`
	DraftID   string `json:"draftId"`
}

// ComposerApp is a lightweight app struct for detached composer windows.
// It connects to the main window via IPC and shares the same database.
type ComposerApp struct {
	ctx    context.Context
	config ComposerConfig

	// Debug mode function reference (injected from main)
	debugMode func() bool

	// IPC client for communication with main window
	ipcClient ipc.Client
	ipcToken  string

	// Database (shared with main window, read-only for most operations)
	db            *database.DB
	accountStore  *account.Store
	folderStore   *folder.Store
	messageStore  *message.Store
	contactStore  *contact.Store
	draftStore    *draft.Store
	credStore     *credentials.Store
	settingsStore *settings.Store

	// IMAP pool for sending/draft operations
	imapPool *imap.Pool

	// OAuth2 manager for token refresh
	oauth2Manager *oauth2.Manager

	// Paths
	paths *platform.Paths

	// Composer state
	originalMessage *message.Message     // For reply/forward
	currentDraft    *draft.Draft         // Current draft being edited
	composeMessage  *smtp.ComposeMessage // Prepared compose message
}

// NewComposerApp creates a new ComposerApp with the given configuration.
func NewComposerApp(config ComposerConfig, debugModeFn func() bool) *ComposerApp {
	return &ComposerApp{
		config:    config,
		debugMode: debugModeFn,
	}
}

// Startup is called when the composer window starts.
func (c *ComposerApp) Startup(ctx context.Context) {
	c.ctx = ctx

	// Initialize logging - fatal only unless --debug flag is used
	logLevel := "fatal"
	if c.debugMode != nil && c.debugMode() {
		logLevel = "debug"
	}
	logging.Init(logging.Config{
		Level:   logLevel,
		Console: true,
	})
	log := logging.WithComponent("composer")

	log.Info().
		Str("accountID", c.config.AccountID).
		Str("mode", c.config.Mode).
		Str("ipcAddress", c.config.IPCAddress).
		Msg("Composer window starting")

	// Get platform paths
	paths, err := platform.GetPaths()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to get platform paths")
	}
	c.paths = paths

	// Open database (shared with main window)
	db, err := database.Open(paths.DatabasePath())
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to open database")
	}
	c.db = db

	// Initialize stores
	c.accountStore = account.NewStore(db)
	c.folderStore = folder.NewStore(db)
	c.messageStore = message.NewStore(db)
	c.contactStore = contact.NewStore(db.DB)
	c.draftStore = draft.NewStore(db)
	c.settingsStore = settings.NewStore(db)

	// Initialize credential store
	credStore, err := credentials.NewStore(db.DB, paths.Data)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize credential store")
	}
	c.credStore = credStore

	// Initialize IMAP pool for send/draft operations
	poolConfig := imap.DefaultPoolConfig()
	poolConfig.MaxConnections = 1 // Composer only needs 1 connection
	c.imapPool = imap.NewPool(poolConfig, c.getIMAPCredentials)

	// Initialize OAuth2 manager for token refresh
	c.oauth2Manager = oauth2.NewManager()

	// Connect to main window's IPC server
	if err := c.connectIPC(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to connect to IPC server")
		// Continue anyway - composer can still work offline
	}

	// Load initial data based on mode
	if err := c.loadInitialData(); err != nil {
		log.Error().Err(err).Msg("Failed to load initial data")
	}

	// Notify main window that we're ready
	c.notifyReady()

	log.Info().Msg("Composer window started successfully")
}

// Shutdown is called when the composer window is closing.
func (c *ComposerApp) Shutdown(ctx context.Context) {
	log := logging.WithComponent("composer")

	// Notify main window that we're closing
	c.notifyClosed()

	// Close IPC connection
	if c.ipcClient != nil {
		c.ipcClient.Close()
	}

	// Close IMAP connections
	if c.imapPool != nil {
		c.imapPool.CloseAll()
	}

	// Close database
	if c.db != nil {
		c.db.Close()
	}

	log.Info().Msg("Composer window shutdown complete")
}

// connectIPC establishes connection to the main window's IPC server.
func (c *ComposerApp) connectIPC(ctx context.Context) error {
	log := logging.WithComponent("composer.ipc")

	// Read token from stdin (passed by parent process)
	tokenBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("failed to read token from stdin: %w", err)
	}
	c.ipcToken = strings.TrimSpace(string(tokenBytes))

	if c.ipcToken == "" {
		return fmt.Errorf("no token provided via stdin")
	}

	log.Debug().Str("address", c.config.IPCAddress).Msg("Connecting to IPC server")

	// Create IPC client
	c.ipcClient = ipc.NewClient(c.config.IPCAddress)

	// Register message handler before connecting
	c.ipcClient.OnMessage(c.handleIPCMessage)

	// Connect with token authentication
	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := c.ipcClient.Connect(connectCtx, c.ipcToken); err != nil {
		return fmt.Errorf("failed to connect to IPC server: %w", err)
	}

	log.Info().Msg("Connected to IPC server")
	return nil
}

// handleIPCMessage processes messages received from the main window.
func (c *ComposerApp) handleIPCMessage(msg ipc.Message) {
	log := logging.WithComponent("composer.ipc")

	log.Debug().Str("type", msg.Type).Msg("Received IPC message")

	switch msg.Type {
	case ipc.TypeThemeChanged:
		var payload ipc.ThemeChangedPayload
		if err := msg.ParsePayload(&payload); err == nil {
			// Emit event to frontend
			wailsRuntime.EventsEmit(c.ctx, "theme:changed", payload.Theme)
		}

	case ipc.TypeAccountUpdated:
		var payload ipc.AccountUpdatedPayload
		if err := msg.ParsePayload(&payload); err == nil {
			// Reload account data if it's our account
			if payload.AccountID == c.config.AccountID {
				wailsRuntime.EventsEmit(c.ctx, "account:updated", payload.AccountID)
			}
		}

	case ipc.TypeContactsUpdated:
		// Emit event to frontend to refresh autocomplete
		wailsRuntime.EventsEmit(c.ctx, "contacts:updated", nil)

	case ipc.TypeShutdown:
		var payload ipc.ShutdownPayload
		msg.ParsePayload(&payload)
		log.Info().Str("reason", payload.Reason).Msg("Received shutdown request from main window")
		// Emit event to frontend to prompt user
		wailsRuntime.EventsEmit(c.ctx, "app:shutdown", payload.Reason)

	default:
		log.Debug().Str("type", msg.Type).Msg("Unknown IPC message type")
	}
}

// loadInitialData loads the initial data based on compose mode.
func (c *ComposerApp) loadInitialData() error {
	log := logging.WithComponent("composer")

	log.Debug().
		Str("draftID", c.config.DraftID).
		Str("messageID", c.config.MessageID).
		Str("mode", c.config.Mode).
		Msg("Loading initial data")

	// If resuming a draft, load it
	if c.config.DraftID != "" {
		draft, err := c.draftStore.Get(c.config.DraftID)
		if err != nil {
			log.Error().Err(err).Str("draftID", c.config.DraftID).Msg("Failed to get draft from store")
			return fmt.Errorf("failed to load draft: %w", err)
		}
		if draft != nil {
			c.currentDraft = draft
			log.Info().
				Str("draftID", c.config.DraftID).
				Str("subject", draft.Subject).
				Uint32("imapUID", draft.IMAPUID).
				Msg("Loaded draft into currentDraft")
		} else {
			log.Warn().Str("draftID", c.config.DraftID).Msg("Draft not found in database")
		}
		return nil
	}

	// If replying/forwarding, load the original message
	if c.config.MessageID != "" && c.config.Mode != "new" {
		msg, err := c.messageStore.Get(c.config.MessageID)
		if err != nil {
			return fmt.Errorf("failed to load original message: %w", err)
		}
		if msg != nil {
			c.originalMessage = msg
			log.Info().Str("messageID", c.config.MessageID).Str("mode", c.config.Mode).Msg("Loaded original message")
		}
	}

	return nil
}

// notifyReady sends a ready notification to the main window.
func (c *ComposerApp) notifyReady() {
	if c.ipcClient == nil {
		return
	}

	msg, err := ipc.NewMessage(ipc.TypeComposerReady, nil)
	if err != nil {
		return
	}
	c.ipcClient.Send(msg)
}

// notifyClosed sends a closed notification to the main window.
func (c *ComposerApp) notifyClosed() {
	if c.ipcClient == nil {
		return
	}

	var draftID *int64
	if c.currentDraft != nil {
		id, _ := parseIntID(c.currentDraft.ID)
		draftID = &id
	}

	msg, err := ipc.NewMessage(ipc.TypeComposerClosed, ipc.ComposerClosedPayload{
		DraftID: draftID,
	})
	if err != nil {
		return
	}
	c.ipcClient.Send(msg)
}

// notifyMessageSent sends a message-sent notification to the main window.
func (c *ComposerApp) notifyMessageSent(folderID int64) {
	if c.ipcClient == nil {
		return
	}

	msg, err := ipc.NewMessage(ipc.TypeMessageSent, ipc.MessageSentPayload{
		AccountID: c.config.AccountID,
		FolderID:  folderID,
	})
	if err != nil {
		return
	}
	c.ipcClient.Send(msg)
}

// notifyDraftSaved sends a draft-saved notification to the main window.
func (c *ComposerApp) notifyDraftSaved(draftID string) {
	if c.ipcClient == nil {
		return
	}

	msg, err := ipc.NewMessage(ipc.TypeDraftSaved, ipc.DraftSavedPayload{
		AccountID: c.config.AccountID,
		DraftID:   draftID,
	})
	if err != nil {
		return
	}
	c.ipcClient.Send(msg)
}

// notifyDraftDeleted sends a draft-deleted notification to the main window.
func (c *ComposerApp) notifyDraftDeleted() {
	if c.ipcClient == nil {
		return
	}

	msg, err := ipc.NewMessage(ipc.TypeDraftDeleted, ipc.DraftDeletedPayload{
		AccountID: c.config.AccountID,
	})
	if err != nil {
		return
	}
	c.ipcClient.Send(msg)
}

// getIMAPCredentials returns IMAP credentials for an account.
// Handles both password and OAuth2 authentication.
func (c *ComposerApp) getIMAPCredentials(accountID string) (*imap.ClientConfig, error) {
	acc, err := c.accountStore.Get(accountID)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return nil, fmt.Errorf("account not found: %s", accountID)
	}

	config := imap.DefaultConfig()
	config.Host = acc.IMAPHost
	config.Port = acc.IMAPPort
	config.Security = imap.SecurityType(acc.IMAPSecurity)
	config.Username = acc.Username

	// Handle authentication based on auth type
	if acc.AuthType == account.AuthOAuth2 {
		// Get valid OAuth token (refreshing if needed)
		tokens, err := c.getValidOAuthToken(accountID)
		if err != nil {
			return nil, fmt.Errorf("failed to get OAuth token: %w", err)
		}
		config.AuthType = imap.AuthTypeOAuth2
		config.AccessToken = tokens.AccessToken
	} else {
		// Default to password authentication
		password, err := c.credStore.GetPassword(accountID)
		if err != nil {
			return nil, fmt.Errorf("failed to get password: %w", err)
		}
		config.AuthType = imap.AuthTypePassword
		config.Password = password
	}

	return &config, nil
}

// getValidOAuthToken returns a valid OAuth token, refreshing if needed.
func (c *ComposerApp) getValidOAuthToken(accountID string) (*credentials.OAuthTokens, error) {
	log := logging.WithComponent("composer")

	tokens, err := c.credStore.GetOAuthTokens(accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get OAuth tokens: %w", err)
	}

	// Check if token expires within 5 minutes
	if tokens.IsExpiringSoon(5 * time.Minute) {
		log.Debug().
			Str("account_id", accountID).
			Time("expires_at", tokens.ExpiresAt).
			Msg("OAuth token expiring soon, refreshing")

		// Refresh the token
		newTokenResp, err := c.oauth2Manager.RefreshToken(tokens.Provider, tokens.RefreshToken)
		if err != nil {
			log.Error().Err(err).
				Str("account_id", accountID).
				Msg("OAuth token refresh failed")

			// Emit event for frontend to prompt re-authorization
			wailsRuntime.EventsEmit(c.ctx, "oauth:reauth-required", map[string]interface{}{
				"accountId": accountID,
				"provider":  tokens.Provider,
				"error":     err.Error(),
			})

			return nil, fmt.Errorf("OAuth token refresh failed, re-authorization required: %w", err)
		}

		// Calculate new expiry time
		expiresAt := time.Now().Add(time.Duration(newTokenResp.ExpiresIn) * time.Second)

		// Update tokens in store
		tokens.AccessToken = newTokenResp.AccessToken
		tokens.ExpiresAt = expiresAt
		if newTokenResp.RefreshToken != "" {
			tokens.RefreshToken = newTokenResp.RefreshToken
		}

		if err := c.credStore.SetOAuthTokens(accountID, tokens); err != nil {
			log.Warn().Err(err).Msg("Failed to save refreshed OAuth tokens")
			// Continue anyway - we have valid tokens in memory
		}

		log.Info().
			Str("account_id", accountID).
			Time("new_expires_at", expiresAt).
			Msg("OAuth token refreshed successfully")
	}

	return tokens, nil
}

// ============================================================================
// Wails-bound methods (exposed to frontend)
// ============================================================================

// GetAccount returns the account for this composer.
func (c *ComposerApp) GetAccount() (*account.Account, error) {
	return c.accountStore.Get(c.config.AccountID)
}

// GetIdentities returns all identities for the account.
func (c *ComposerApp) GetIdentities() ([]*account.Identity, error) {
	return c.accountStore.GetIdentities(c.config.AccountID)
}

// GetComposeMode returns the compose mode and related data.
func (c *ComposerApp) GetComposeMode() *ComposeMode {
	return &ComposeMode{
		AccountID: c.config.AccountID,
		Mode:      c.config.Mode,
		MessageID: c.config.MessageID,
		DraftID:   c.config.DraftID,
	}
}

// GetThemeMode returns the current theme mode setting.
func (c *ComposerApp) GetThemeMode() (string, error) {
	return c.settingsStore.GetThemeMode()
}

// GetOriginalMessage returns the original message for reply/forward.
func (c *ComposerApp) GetOriginalMessage() (*message.Message, error) {
	if c.originalMessage != nil {
		return c.originalMessage, nil
	}
	if c.config.MessageID == "" {
		return nil, nil
	}
	return c.messageStore.Get(c.config.MessageID)
}

// GetDraft returns the current draft being edited.
func (c *ComposerApp) GetDraft() (*smtp.ComposeMessage, error) {
	if c.currentDraft == nil {
		return nil, nil
	}
	return c.draftToComposeMessage(c.currentDraft), nil
}

// PrepareReply builds a ComposeMessage for the current mode.
func (c *ComposerApp) PrepareReply() (*smtp.ComposeMessage, error) {
	if c.config.Mode == "new" || c.config.MessageID == "" {
		// New message - return empty compose
		acc, err := c.accountStore.Get(c.config.AccountID)
		if err != nil {
			return nil, err
		}
		identities, _ := c.accountStore.GetIdentities(c.config.AccountID)

		var fromIdentity *account.Identity
		for _, id := range identities {
			if id.IsDefault {
				fromIdentity = id
				break
			}
		}
		if fromIdentity == nil && len(identities) > 0 {
			fromIdentity = identities[0]
		}

		from := smtp.Address{Address: acc.Email, Name: acc.Name}
		if fromIdentity != nil {
			from = smtp.Address{Address: fromIdentity.Email, Name: fromIdentity.Name}
		}

		return &smtp.ComposeMessage{
			From: from,
		}, nil
	}

	// For reply/forward, use the same logic as main app
	// This is a simplified version - the full logic is in app.go PrepareReply
	msg, err := c.messageStore.Get(c.config.MessageID)
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, fmt.Errorf("message not found: %s", c.config.MessageID)
	}

	c.originalMessage = msg
	c.composeMessage = c.buildReplyMessage(msg, c.config.Mode)
	return c.composeMessage, nil
}

// SearchContacts searches for contacts matching the query.
func (c *ComposerApp) SearchContacts(query string, limit int) ([]*contact.Contact, error) {
	return c.contactStore.Search(query, limit)
}

// SendMessage sends the composed email.
func (c *ComposerApp) SendMessage(msg smtp.ComposeMessage) error {
	log := logging.WithComponent("composer")

	log.Info().
		Str("from", msg.From.Address).
		Int("toCount", len(msg.To)).
		Str("subject", msg.Subject).
		Msg("Sending message")

	// Get account for SMTP settings
	acc, err := c.accountStore.Get(c.config.AccountID)
	if err != nil {
		return fmt.Errorf("failed to get account: %w", err)
	}

	// Build RFC822 message
	rawMsg, err := msg.ToRFC822()
	if err != nil {
		return fmt.Errorf("failed to build message: %w", err)
	}

	// Create SMTP client config
	smtpConfig := smtp.DefaultConfig()
	smtpConfig.Host = acc.SMTPHost
	smtpConfig.Port = acc.SMTPPort
	smtpConfig.Security = smtp.SecurityType(acc.SMTPSecurity)
	smtpConfig.Username = acc.Username

	// Handle authentication based on auth type
	if acc.AuthType == account.AuthOAuth2 {
		// Get valid OAuth token (refreshing if needed)
		tokens, err := c.getValidOAuthToken(c.config.AccountID)
		if err != nil {
			return fmt.Errorf("failed to get OAuth token: %w", err)
		}
		smtpConfig.AuthType = smtp.AuthTypeOAuth2
		smtpConfig.AccessToken = tokens.AccessToken
	} else {
		// Default to password authentication
		password, err := c.credStore.GetPassword(c.config.AccountID)
		if err != nil {
			return fmt.Errorf("failed to get password: %w", err)
		}
		smtpConfig.AuthType = smtp.AuthTypePassword
		smtpConfig.Password = password
	}

	client := smtp.NewClient(smtpConfig)

	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to SMTP: %w", err)
	}
	defer client.Close()

	if err := client.Login(); err != nil {
		return fmt.Errorf("failed to login to SMTP: %w", err)
	}

	recipients := msg.AllRecipients()
	if len(recipients) == 0 {
		return fmt.Errorf("no recipients")
	}

	if err := client.SendMail(msg.From.Address, recipients, rawMsg); err != nil {
		return fmt.Errorf("failed to send: %w", err)
	}

	// Save to Sent folder if provider doesn't auto-save
	if !providerAutoSavesSentMail(acc.IMAPHost) {
		log.Debug().Str("host", acc.IMAPHost).Msg("Provider doesn't auto-save, using IMAP APPEND")
		if err := c.saveToSentFolder(acc, rawMsg); err != nil {
			log.Warn().Err(err).Msg("Failed to save message to Sent folder")
		}
	} else {
		log.Debug().Str("host", acc.IMAPHost).Msg("Provider auto-saves sent mail")
	}

	// Add recipients to contacts
	for _, to := range msg.To {
		c.contactStore.AddOrUpdate(to.Address, to.Name)
	}
	for _, cc := range msg.Cc {
		c.contactStore.AddOrUpdate(cc.Address, cc.Name)
	}

	// Delete draft if we were editing one
	if c.currentDraft != nil {
		c.draftStore.Delete(c.currentDraft.ID)
	}

	// Get sent folder ID for notification
	sentFolder, _ := c.folderStore.GetByType(c.config.AccountID, folder.TypeSent)
	var sentFolderID int64
	if sentFolder != nil {
		sentFolderID, _ = parseIntID(sentFolder.ID)
	}

	// Notify main window
	c.notifyMessageSent(sentFolderID)

	log.Info().Msg("Message sent successfully")
	return nil
}

// saveToSentFolder appends the sent message to the Sent folder via IMAP.
// Used for providers that don't automatically save sent messages.
func (c *ComposerApp) saveToSentFolder(acc *account.Account, rawMsg []byte) error {
	log := logging.WithComponent("composer")

	// Get the Sent folder path
	sentFolder, err := c.folderStore.GetByType(acc.ID, folder.TypeSent)
	if err != nil || sentFolder == nil {
		if acc.SentFolderPath == "" {
			return fmt.Errorf("no Sent folder configured or detected")
		}
	}

	sentPath := acc.SentFolderPath
	if sentPath == "" && sentFolder != nil {
		sentPath = sentFolder.Path
	}

	log.Debug().
		Str("account_id", acc.ID).
		Str("sent_path", sentPath).
		Msg("Saving sent message to folder via IMAP APPEND")

	// Create IMAP client
	clientConfig := imap.DefaultConfig()
	clientConfig.Host = acc.IMAPHost
	clientConfig.Port = acc.IMAPPort
	clientConfig.Security = imap.SecurityType(acc.IMAPSecurity)
	clientConfig.Username = acc.Username

	// Handle authentication
	if acc.AuthType == account.AuthOAuth2 {
		tokens, err := c.getValidOAuthToken(acc.ID)
		if err != nil {
			return fmt.Errorf("failed to get OAuth token: %w", err)
		}
		clientConfig.AuthType = imap.AuthTypeOAuth2
		clientConfig.AccessToken = tokens.AccessToken
	} else {
		password, err := c.credStore.GetPassword(acc.ID)
		if err != nil {
			return fmt.Errorf("failed to get password: %w", err)
		}
		clientConfig.AuthType = imap.AuthTypePassword
		clientConfig.Password = password
	}

	imapClient := imap.NewClient(clientConfig)
	if err := imapClient.Connect(); err != nil {
		return fmt.Errorf("failed to connect to IMAP: %w", err)
	}
	defer imapClient.Close()

	if err := imapClient.Login(); err != nil {
		return fmt.Errorf("failed to login to IMAP: %w", err)
	}

	// Append message with \Seen flag
	flags := []goImap.Flag{goImap.FlagSeen}
	_, err = imapClient.AppendMessage(sentPath, flags, time.Now(), rawMsg)
	if err != nil {
		return fmt.Errorf("failed to append to Sent folder: %w", err)
	}

	log.Info().
		Str("account_id", acc.ID).
		Str("sent_path", sentPath).
		Msg("Message saved to Sent folder")

	return nil
}

// SaveDraft saves the current compose state as a draft.
// If existingDraftID is provided, updates that draft instead of creating a new one.
func (c *ComposerApp) SaveDraft(msg smtp.ComposeMessage, existingDraftID string) (*draft.Draft, error) {
	log := logging.WithComponent("composer")

	log.Debug().
		Str("existingDraftID", existingDraftID).
		Str("subject", msg.Subject).
		Msg("Saving draft")

	var localDraft *draft.Draft

	// Try to load existing draft if ID provided
	if existingDraftID != "" {
		existing, err := c.draftStore.Get(existingDraftID)
		if err != nil {
			log.Warn().Err(err).Str("draftID", existingDraftID).Msg("Failed to load existing draft from ID")
		} else if existing != nil {
			localDraft = existing
			log.Debug().Str("draftID", existingDraftID).Msg("Loaded existing draft from provided ID")
		}
	}

	// Fall back to c.currentDraft if no draft loaded yet
	if localDraft == nil && c.currentDraft != nil {
		localDraft = c.currentDraft
		log.Debug().Str("draftID", localDraft.ID).Msg("Using c.currentDraft")
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

		if err := c.draftStore.Update(localDraft); err != nil {
			return nil, fmt.Errorf("failed to update draft: %w", err)
		}
		log.Debug().Str("draftID", localDraft.ID).Msg("Updated existing draft")
	} else {
		// Create new draft
		localDraft = &draft.Draft{
			AccountID:   c.config.AccountID,
			ToList:      addressListToJSON(msg.To),
			CcList:      addressListToJSON(msg.Cc),
			BccList:     addressListToJSON(msg.Bcc),
			Subject:     msg.Subject,
			BodyHTML:    msg.HTMLBody,
			BodyText:    msg.TextBody,
			InReplyToID: msg.InReplyTo,
			SyncStatus:  draft.SyncStatusPending,
		}

		if err := c.draftStore.Create(localDraft); err != nil {
			return nil, fmt.Errorf("failed to create draft: %w", err)
		}
		log.Debug().Str("draftID", localDraft.ID).Msg("Created new draft")
	}

	// Keep c.currentDraft in sync
	c.currentDraft = localDraft

	// Sync to IMAP in background (notifies main window after successful upload)
	go c.syncDraftToIMAP(localDraft, msg)

	log.Info().Str("draftID", localDraft.ID).Msg("Draft saved")
	return localDraft, nil
}

// DeleteDraft deletes a draft from local DB and IMAP.
// If draftID is empty, falls back to c.currentDraft.ID or c.config.DraftID.
func (c *ComposerApp) DeleteDraft(draftID string) error {
	log := logging.WithComponent("composer")

	// Determine which draft ID to use
	if draftID == "" {
		if c.currentDraft != nil {
			draftID = c.currentDraft.ID
		} else if c.config.DraftID != "" {
			draftID = c.config.DraftID
		}
	}

	if draftID == "" {
		log.Debug().Msg("No draft ID provided, nothing to delete")
		return nil
	}

	log.Debug().Str("draftID", draftID).Msg("DeleteDraft called")

	// Load the draft directly from database (don't rely on c.currentDraft state)
	draftToDelete, err := c.draftStore.Get(draftID)
	if err != nil {
		log.Warn().Err(err).Str("draftID", draftID).Msg("Failed to load draft for deletion")
		return fmt.Errorf("failed to load draft: %w", err)
	}
	if draftToDelete == nil {
		log.Debug().Str("draftID", draftID).Msg("Draft not found in database, nothing to delete")
		return nil
	}

	log.Info().
		Str("draftID", draftToDelete.ID).
		Uint32("imapUID", draftToDelete.IMAPUID).
		Str("syncStatus", string(draftToDelete.SyncStatus)).
		Msg("Deleting draft")

	// If synced to IMAP, delete from server first
	if draftToDelete.IsSynced() {
		draftsFolder, _ := c.folderStore.GetByType(c.config.AccountID, folder.TypeDrafts)
		if draftsFolder != nil {
			poolConn, err := c.imapPool.GetConnection(c.ctx, c.config.AccountID)
			if err == nil {
				defer c.imapPool.Release(poolConn)
				conn := poolConn.Client()
				if _, err := conn.SelectMailbox(c.ctx, draftsFolder.Path); err == nil {
					if err := conn.DeleteMessageByUID(goImap.UID(draftToDelete.IMAPUID)); err != nil {
						log.Warn().Err(err).Uint32("uid", draftToDelete.IMAPUID).Msg("Failed to delete draft from IMAP")
					} else {
						log.Info().Uint32("uid", draftToDelete.IMAPUID).Msg("Draft deleted from IMAP")
					}
				}
			} else {
				log.Warn().Err(err).Msg("Failed to get IMAP connection for draft deletion")
			}
		}
	}

	// Delete from local database
	if err := c.draftStore.Delete(draftToDelete.ID); err != nil {
		return fmt.Errorf("failed to delete draft from database: %w", err)
	}

	// Notify main window to refresh Drafts folder
	c.notifyDraftDeleted()

	log.Info().Str("draftID", draftToDelete.ID).Msg("Draft deleted successfully")

	// Clear currentDraft if it matches
	if c.currentDraft != nil && c.currentDraft.ID == draftToDelete.ID {
		c.currentDraft = nil
	}

	return nil
}

// syncDraftToIMAP syncs a draft to the IMAP server.
// This runs in a background goroutine and emits events to this window's frontend.
func (c *ComposerApp) syncDraftToIMAP(localDraft *draft.Draft, msg smtp.ComposeMessage) {
	log := logging.WithComponent("composer")

	// Helper to emit sync status change event to this window
	emitSyncStatus := func(status string, imapUID uint32, syncError string) {
		wailsRuntime.EventsEmit(c.ctx, "draft:syncStatusChanged", map[string]interface{}{
			"draftId":    localDraft.ID,
			"syncStatus": status,
			"imapUid":    imapUID,
			"error":      syncError,
		})
	}

	// Find the Drafts folder for this account
	draftsFolder, err := c.folderStore.GetByType(c.config.AccountID, folder.TypeDrafts)
	if err != nil || draftsFolder == nil {
		log.Warn().Err(err).Str("account_id", c.config.AccountID).Msg("No drafts folder found, skipping IMAP sync")
		c.draftStore.UpdateSyncStatus(localDraft.ID, draft.SyncStatusFailed, 0, "", "no drafts folder found")
		emitSyncStatus("failed", 0, "no drafts folder found")
		return
	}

	// Get IMAP connection from pool
	poolConn, err := c.imapPool.GetConnection(c.ctx, c.config.AccountID)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get IMAP connection, will retry later")
		c.draftStore.UpdateSyncStatus(localDraft.ID, draft.SyncStatusFailed, 0, "", err.Error())
		emitSyncStatus("failed", 0, err.Error())
		return
	}
	defer c.imapPool.Release(poolConn)

	conn := poolConn.Client()

	// Delete old IMAP draft if it exists
	if localDraft.IMAPUID > 0 && localDraft.FolderID != "" {
		if _, err := conn.SelectMailbox(c.ctx, draftsFolder.Path); err == nil {
			if err := conn.DeleteMessageByUID(goImap.UID(localDraft.IMAPUID)); err != nil {
				log.Warn().Err(err).Uint32("uid", localDraft.IMAPUID).Msg("Failed to delete old draft from IMAP")
			}
		}
	}

	// Build RFC822 message
	rawMsg, err := msg.ToRFC822()
	if err != nil {
		log.Error().Err(err).Msg("Failed to build RFC822 message")
		c.draftStore.UpdateSyncStatus(localDraft.ID, draft.SyncStatusFailed, 0, "", err.Error())
		emitSyncStatus("failed", 0, err.Error())
		return
	}

	// Append to IMAP Drafts folder with \Draft and \Seen flags
	flags := []goImap.Flag{goImap.FlagDraft, goImap.FlagSeen}
	uid, err := conn.AppendMessage(draftsFolder.Path, flags, time.Now(), rawMsg)
	if err != nil {
		log.Error().Err(err).Msg("Failed to append draft to IMAP")
		c.draftStore.UpdateSyncStatus(localDraft.ID, draft.SyncStatusFailed, 0, "", err.Error())
		emitSyncStatus("failed", 0, err.Error())
		return
	}

	// Update local draft with sync status
	err = c.draftStore.UpdateSyncStatus(localDraft.ID, draft.SyncStatusSynced, uint32(uid), draftsFolder.ID, "")
	if err != nil {
		log.Warn().Err(err).Msg("Failed to update draft sync status")
	}

	// Emit success event to this window
	emitSyncStatus("synced", uint32(uid), "")

	log.Info().
		Str("id", localDraft.ID).
		Uint32("imap_uid", uint32(uid)).
		Msg("Draft synced to IMAP successfully")

	// Notify main window now that the draft is on IMAP
	// This triggers the main window to sync the Drafts folder
	c.notifyDraftSaved(localDraft.ID)
}

// CloseWindow requests the window to close.
func (c *ComposerApp) CloseWindow() {
	wailsRuntime.Quit(c.ctx)
}

// PickAttachmentFiles opens a file picker dialog and returns the selected files as attachments.
func (c *ComposerApp) PickAttachmentFiles() ([]ComposerAttachment, error) {
	log := logging.WithComponent("composer")

	// Show multi-file picker dialog
	files, err := wailsRuntime.OpenMultipleFilesDialog(c.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Select Attachments",
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to show file picker dialog")
		return nil, fmt.Errorf("failed to show file picker: %w", err)
	}

	// User cancelled
	if len(files) == 0 {
		return nil, nil
	}

	var attachments []ComposerAttachment
	for _, filePath := range files {
		att, err := c.readFileAsAttachment(filePath)
		if err != nil {
			log.Warn().Err(err).Str("path", filePath).Msg("Failed to read file as attachment")
			continue
		}
		attachments = append(attachments, *att)
	}

	log.Info().Int("count", len(attachments)).Msg("Files picked for attachment")
	return attachments, nil
}

// readFileAsAttachment reads a file and creates a ComposerAttachment.
func (c *ComposerApp) readFileAsAttachment(filePath string) (*ComposerAttachment, error) {
	log := logging.WithComponent("composer")

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Get filename
	filename := filepath.Base(filePath)

	// Detect content type from extension
	contentType := detectContentType(filename)

	// Encode to base64 for JSON transport
	encoded := base64.StdEncoding.EncodeToString(content)

	log.Debug().
		Str("filename", filename).
		Str("contentType", contentType).
		Int("size", len(content)).
		Msg("File read as attachment")

	return &ComposerAttachment{
		Filename:    filename,
		ContentType: contentType,
		Size:        len(content),
		Data:        encoded,
	}, nil
}

// ============================================================================
// Helper methods
// ============================================================================

// draftToComposeMessage converts a draft to a ComposeMessage.
func (c *ComposerApp) draftToComposeMessage(d *draft.Draft) *smtp.ComposeMessage {
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

// buildReplyMessage builds a compose message for reply/forward.
// This is a simplified version of the logic in app.go PrepareReply.
func (c *ComposerApp) buildReplyMessage(msg *message.Message, mode string) *smtp.ComposeMessage {
	// Get default identity
	identities, _ := c.accountStore.GetIdentities(c.config.AccountID)
	var fromIdentity *account.Identity
	for _, id := range identities {
		if id.IsDefault {
			fromIdentity = id
			break
		}
	}
	if fromIdentity == nil && len(identities) > 0 {
		fromIdentity = identities[0]
	}

	from := smtp.Address{}
	if fromIdentity != nil {
		from = smtp.Address{Name: fromIdentity.Name, Address: fromIdentity.Email}
	}

	// Build subject
	subject := msg.Subject
	switch mode {
	case "forward":
		if !strings.HasPrefix(strings.ToLower(subject), "fwd:") && !strings.HasPrefix(strings.ToLower(subject), "fw:") {
			subject = "Fwd: " + subject
		}
	default: // reply, reply-all
		if !strings.HasPrefix(strings.ToLower(subject), "re:") {
			subject = "Re: " + subject
		}
	}

	// Build recipients
	var to, cc []smtp.Address
	selfEmails := make(map[string]bool)
	for _, id := range identities {
		selfEmails[strings.ToLower(id.Email)] = true
	}

	originalFrom := []smtp.Address{{Name: msg.FromName, Address: msg.FromEmail}}

	switch mode {
	case "reply":
		to = filterSelfAddresses(originalFrom, selfEmails)
	case "reply-all":
		to = filterSelfAddresses(originalFrom, selfEmails)
		// Add original To (excluding self)
		originalTo := parseAddressList(msg.ToList)
		to = append(to, filterSelfAddresses(originalTo, selfEmails)...)
		// Add original Cc (excluding self and duplicates)
		originalCc := parseAddressList(msg.CcList)
		toSet := make(map[string]bool)
		for _, addr := range to {
			toSet[strings.ToLower(addr.Address)] = true
		}
		for _, addr := range filterSelfAddresses(originalCc, selfEmails) {
			if !toSet[strings.ToLower(addr.Address)] {
				cc = append(cc, addr)
			}
		}
	case "forward":
		// Leave empty for user to fill
	}

	// Build quoted body
	dateStr := msg.Date.Format("Mon, Jan 2 2006 at 3:04:05 PM MST")
	sender := msg.FromEmail
	if msg.FromName != "" {
		sender = msg.FromName + " <" + msg.FromEmail + ">"
	}

	var htmlBody, textBody string
	if mode == "forward" {
		htmlBody = fmt.Sprintf("<br><br>---------- Forwarded message ----------<br>From: %s<br>Subject: %s<br>Date: %s<br>To: %s<br><br>%s",
			escapeHTML(sender), escapeHTML(msg.Subject), escapeHTML(dateStr), escapeHTML(msg.ToList), msg.BodyHTML)
		textBody = fmt.Sprintf("\n\n---------- Forwarded message ----------\nFrom: %s\nSubject: %s\nDate: %s\nTo: %s\n\n%s",
			sender, msg.Subject, dateStr, msg.ToList, msg.BodyText)
	} else {
		citation := fmt.Sprintf("On %s, %s wrote:", dateStr, sender)
		htmlBody = fmt.Sprintf("<br><br>%s<br><blockquote type=\"cite\">%s</blockquote>", escapeHTML(citation), msg.BodyHTML)
		textBody = fmt.Sprintf("\n\n%s\n%s", citation, quoteText(msg.BodyText))
	}

	return &smtp.ComposeMessage{
		From:      from,
		To:        to,
		Cc:        cc,
		Subject:   subject,
		HTMLBody:  htmlBody,
		TextBody:  textBody,
		InReplyTo: msg.MessageID,
	}
}

// parseIntID parses a string ID to int64.
func parseIntID(id string) (int64, error) {
	var result int64
	_, err := fmt.Sscanf(id, "%d", &result)
	return result, err
}
