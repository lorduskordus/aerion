package app

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	goSync "sync"
	"time"

	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/appstate"
	"github.com/hkdb/aerion/internal/carddav"
	"github.com/hkdb/aerion/internal/certificate"
	"github.com/hkdb/aerion/internal/contact"
	"github.com/hkdb/aerion/internal/credentials"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/draft"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/imap"
	"github.com/hkdb/aerion/internal/ipc"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/notification"
	"github.com/hkdb/aerion/internal/oauth2"
	"github.com/hkdb/aerion/internal/platform"
	"github.com/hkdb/aerion/internal/settings"
	"github.com/hkdb/aerion/internal/pgp"
	"github.com/hkdb/aerion/internal/smime"
	"github.com/hkdb/aerion/internal/sync"
	"github.com/hkdb/aerion/internal/undo"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// MailtoData holds parsed mailto: URL data
type MailtoData struct {
	To      []string `json:"to"`
	Cc      []string `json:"cc"`
	Bcc     []string `json:"bcc"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
}

// App struct holds the application state and dependencies
type App struct {
	ctx context.Context

	// Paths
	paths *platform.Paths

	// Database
	db *database.DB

	// Stores
	accountStore        *account.Store
	folderStore         *folder.Store
	messageStore        *message.Store
	attachmentStore     *message.AttachmentStore
	contactStore        *contact.Store
	draftStore          *draft.Store
	settingsStore       *settings.Store
	appStateStore       *appstate.Store
	imageAllowlistStore *settings.ImageAllowlistStore

	// IMAP
	imapPool   *imap.Pool
	syncEngine *sync.Engine

	// Background sync (polling + IDLE)
	syncScheduler *sync.Scheduler
	idleManager   *imap.IdleManager

	// Credentials (keyring with fallback)
	credStore *credentials.Store

	// Certificate trust store (TOFU)
	certStore *certificate.Store

	// CardDAV
	carddavStore     *carddav.Store
	carddavSyncer    *carddav.Syncer
	carddavScheduler *carddav.Scheduler

	// S/MIME
	smimeStore     *smime.Store
	smimeSigner    *smime.Signer
	smimeVerifier  *smime.Verifier
	smimeEncryptor *smime.Encryptor
	smimeDecryptor *smime.Decryptor

	// PGP
	pgpStore     *pgp.Store
	pgpSigner    *pgp.Signer
	pgpVerifier  *pgp.Verifier
	pgpEncryptor *pgp.Encryptor
	pgpDecryptor *pgp.Decryptor

	// Undo system
	undoStack *undo.Stack

	// IPC for multi-window support (composer windows)
	ipcServer   ipc.Server
	ipcTokenMgr *ipc.TokenManager

	// OAuth2 manager
	oauth2Manager *oauth2.Manager

	// Temporary OAuth token storage (for pending account creation)
	pendingOAuthTokens *oauth2.TokenResponse
	pendingOAuthEmail  string

	// Temporary OAuth token storage (for pending contact source creation)
	pendingContactSourceOAuthTokens   *oauth2.TokenResponse
	pendingContactSourceOAuthEmail    string
	pendingContactSourceOAuthProvider string

	// Google Contacts API client (for OAuth accounts)
	googleContactsClient *contact.GoogleContactsClient

	// Pending mailto: URL data (from command line)
	PendingMailto *MailtoData

	// Full-text search indexer
	ftsIndexer *message.FTSIndexer

	// Sync management - tracks active syncs per account for cancel-and-restart
	syncContexts    map[string]context.CancelFunc // keyed by "accountID:folderID"
	syncLastRequest map[string]time.Time          // last sync request time for debounce
	syncMu          goSync.Mutex                  // protects sync maps

	// Sleep/wake detection for auto-sync on wake
	sleepWakeMonitor platform.SleepWakeMonitor

	// System theme detection (XDG Settings Portal on Linux)
	themeMonitor platform.ThemeMonitor

	// Desktop notifications with click handling
	notifier notification.Notifier

	// DebugMode function reference (injected from main)
	debugMode func() bool

	// UseDirectDBus forces direct D-Bus notifications instead of portal (Linux only)
	useDirectDBus bool
}

// NewApp creates a new App application struct
func NewApp(debugModeFn func() bool, useDirectDBus bool) *App {
	return &App{
		debugMode:     debugModeFn,
		useDirectDBus: useDirectDBus,
	}
}

// shuttingDown tracks if shutdown has been initiated to prevent multiple triggers
var shuttingDown bool

// Startup is called when the app starts
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx

	// Initialize logging - fatal only unless --debug flag is used
	logLevel := "fatal"
	if a.debugMode != nil && a.debugMode() {
		logLevel = "debug"
	}
	logging.Init(logging.Config{
		Level:   logLevel,
		Console: true,
	})
	log := logging.WithComponent("app")

	// Get platform paths
	paths, err := platform.GetPaths()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to get platform paths")
	}
	a.paths = paths

	// Ensure directories exist
	if err := paths.EnsureDirectories(); err != nil {
		log.Fatal().Err(err).Msg("Failed to create directories")
	}
	log.Info().
		Str("config", paths.Config).
		Str("data", paths.Data).
		Str("cache", paths.Cache).
		Msg("Initialized paths")

	// Open database
	db, err := database.Open(paths.DatabasePath())
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to open database")
	}
	a.db = db
	log.Info().Str("path", paths.DatabasePath()).Msg("Opened database")

	// Run migrations
	if err := db.Migrate(); err != nil {
		log.Fatal().Err(err).Msg("Failed to run migrations")
	}
	log.Info().Msg("Database migrations complete")

	// Initialize stores
	a.accountStore = account.NewStore(db)
	a.folderStore = folder.NewStore(db)
	a.messageStore = message.NewStore(db)
	a.attachmentStore = message.NewAttachmentStore(db)
	a.contactStore = contact.NewStore(db.DB)
	a.draftStore = draft.NewStore(db)
	a.settingsStore = settings.NewStore(db)
	a.appStateStore = appstate.NewStore(db.DB)
	a.imageAllowlistStore = settings.NewImageAllowlistStore(db)

	// Scale database connection pool based on number of accounts
	a.updateDBConnectionPool()

	// Initialize vCard scanner for contact autocomplete
	// Scans known Linux paths for .vcf files with 20 minute cache TTL
	vcardScanner := contact.NewVCardScanner(contact.DefaultVCardPaths(), 20*time.Minute)
	a.contactStore.SetVCardScanner(vcardScanner)
	// Trigger initial scan in background
	go vcardScanner.Scan()

	// Initialize CardDAV support (will be fully set up after credStore is initialized)
	a.carddavStore = carddav.NewStore(db.DB)

	// Initialize credential store (keyring with encrypted DB fallback)
	credStore, err := credentials.NewStore(db.DB, paths.Data)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize credential store")
	}
	a.credStore = credStore

	// Initialize certificate trust store (TOFU)
	a.certStore = certificate.NewStore(db.DB)

	// Initialize S/MIME support
	a.smimeStore = smime.NewStore(db.DB, log)
	a.smimeSigner = smime.NewSigner(a.smimeStore, a.credStore, log)
	a.smimeVerifier = smime.NewVerifier(a.smimeStore, log)
	a.smimeEncryptor = smime.NewEncryptor(a.smimeStore, a.credStore, log)
	a.smimeDecryptor = smime.NewDecryptor(a.smimeStore, a.credStore, log)

	// Initialize PGP support
	a.pgpStore = pgp.NewStore(db.DB, log)
	a.pgpSigner = pgp.NewSigner(a.pgpStore, a.credStore, log)
	a.pgpVerifier = pgp.NewVerifier(a.pgpStore, log)
	a.pgpEncryptor = pgp.NewEncryptor(a.pgpStore, a.credStore, log)
	a.pgpDecryptor = pgp.NewDecryptor(a.pgpStore, a.credStore, log)

	// Initialize IMAP connection pool
	poolConfig := imap.DefaultPoolConfig()
	a.imapPool = imap.NewPool(poolConfig, a.getIMAPCredentials)

	// Initialize sync engine
	a.syncEngine = sync.NewEngine(a.imapPool, a.folderStore, a.messageStore, a.attachmentStore)

	// Wire S/MIME and PGP verifiers into sync engine for signature verification during body parsing
	a.syncEngine.SetSMIMEVerifier(a.smimeVerifier)
	a.syncEngine.SetPGPVerifier(a.pgpVerifier)

	// Set up sync progress callback to emit events to frontend
	a.syncEngine.SetProgressCallback(func(progress sync.SyncProgress) {
		wailsRuntime.EventsEmit(ctx, "sync:progress", map[string]interface{}{
			"accountId": progress.AccountID,
			"folderId":  progress.FolderID,
			"fetched":   progress.Fetched,
			"total":     progress.Total,
			"phase":     progress.Phase,
		})
	})

	// Start connection pool cleanup routine
	a.imapPool.StartCleanupRoutine(ctx)

	// Start periodic WAL checkpoint routine to prevent WAL file from growing too large
	go a.db.StartCheckpointRoutine(ctx)

	// Initialize CardDAV syncer and scheduler
	a.carddavSyncer = carddav.NewSyncer(a.carddavStore, a.credStore)
	a.carddavScheduler = carddav.NewScheduler(a.carddavSyncer, a.carddavStore)

	// Set up access token getters for OAuth contact sources
	a.carddavSyncer.SetAccessTokenGetters(
		// Account token getter - for sources linked to email accounts
		func(accountID string) (string, error) {
			tokens, err := a.getValidOAuthToken(accountID)
			if err != nil {
				return "", err
			}
			return tokens.AccessToken, nil
		},
		// Source token getter - for standalone contact sources
		func(sourceID string) (string, error) {
			return a.getValidContactSourceOAuthToken(sourceID)
		},
	)

	// Set up CardDAV search function for contact autocomplete
	a.contactStore.SetCardDAVSearchFunc(func(query string, limit int) ([]*contact.Contact, error) {
		contacts, err := a.carddavStore.SearchContacts(query, limit)
		if err != nil {
			return nil, err
		}
		result := make([]*contact.Contact, len(contacts))
		for i, c := range contacts {
			result[i] = &contact.Contact{
				Email:       c.Email,
				DisplayName: c.DisplayName,
				Source:      "carddav",
			}
		}
		return result, nil
	})

	// Start CardDAV background sync scheduler
	a.carddavScheduler.Start(ctx)

	// Initialize undo stack (max 50 commands, 30 second timeout)
	a.undoStack = undo.NewStack(50, 30*time.Second)

	// Initialize OAuth2 manager for token refresh
	a.oauth2Manager = oauth2.NewManager()

	// Initialize Google Contacts client for OAuth account contact search
	a.googleContactsClient = contact.NewGoogleContactsClient()

	// Initialize IPC for multi-window support
	a.initIPC(ctx)

	// Initialize and start background email sync (polling + IDLE)
	a.initBackgroundSync(ctx)

	// Sync any pending drafts from previous sessions
	go a.syncAllPendingDrafts()

	// Initialize FTS indexer for full-text search
	a.ftsIndexer = message.NewFTSIndexer(db.DB)

	// Initialize sync context tracking for cancel-and-restart
	a.syncContexts = make(map[string]context.CancelFunc)
	a.syncLastRequest = make(map[string]time.Time)

	// Initialize desktop notifications with click handling
	a.initNotifications(ctx)

	// Initialize sleep/wake monitor for auto-sync on wake
	a.initSleepWakeMonitor(ctx)

	// Initialize system theme monitor (XDG Settings Portal on Linux)
	a.initThemeMonitor(ctx)

	// Set up FTS progress callback to emit events to frontend
	a.ftsIndexer.SetProgressCallback(func(folderID string, indexed, total int) {
		percentage := 0
		if total > 0 {
			percentage = (indexed * 100) / total
		}
		wailsRuntime.EventsEmit(ctx, "fts:progress", map[string]interface{}{
			"folderId":   folderID,
			"indexed":    indexed,
			"total":      total,
			"percentage": percentage,
		})
	})

	a.ftsIndexer.SetCompleteCallback(func(folderID string) {
		wailsRuntime.EventsEmit(ctx, "fts:complete", map[string]interface{}{
			"folderId": folderID,
		})
	})

	// Start background FTS indexing after a short delay to let initial sync complete
	go func() {
		time.Sleep(5 * time.Second)
		log.Info().Msg("Starting background FTS indexing")
		wailsRuntime.EventsEmit(ctx, "fts:indexing", map[string]interface{}{
			"status": "started",
		})
		if err := a.ftsIndexer.IndexAllFolders(ctx); err != nil {
			log.Error().Err(err).Msg("Background FTS indexing failed")
		} else {
			log.Info().Msg("Background FTS indexing completed")
			wailsRuntime.EventsEmit(ctx, "fts:indexing", map[string]interface{}{
				"status": "completed",
			})
		}
	}()

	log.Info().Msg("Aerion started successfully")
}

// BeforeClose is called when the window is about to close (e.g., OS close signal)
func (a *App) BeforeClose(ctx context.Context) bool {
	if shuttingDown {
		// Already shutting down, allow the close
		return false
	}

	log := logging.WithComponent("app")
	log.Info().Msg("Window close requested, showing shutdown overlay")

	shuttingDown = true

	// Emit event to show shutdown overlay
	wailsRuntime.EventsEmit(a.ctx, "app:shutting-down")

	// Schedule actual quit after UI has time to render
	go func() {
		time.Sleep(150 * time.Millisecond)
		wailsRuntime.Quit(a.ctx)
	}()

	// Prevent immediate close
	return true
}

// InitiateShutdown triggers the application quit (called from frontend)
func (a *App) InitiateShutdown() {
	if shuttingDown {
		return
	}
	shuttingDown = true

	log := logging.WithComponent("app")
	log.Info().Msg("Initiating shutdown")
	wailsRuntime.Quit(a.ctx)
}

// Shutdown is called when the app is closing
func (a *App) Shutdown(ctx context.Context) {
	log := logging.WithComponent("app")

	// Broadcast shutdown to all composer windows
	if a.ipcServer != nil {
		clients := a.ipcServer.Clients()
		if len(clients) > 0 {
			log.Info().Int("count", len(clients)).Msg("Notifying composer windows of shutdown")
			msg, _ := ipc.NewMessage(ipc.TypeShutdown, ipc.ShutdownPayload{
				Reason: "main window closing",
			})
			a.ipcServer.Broadcast(msg)
			// Give composers a moment to save drafts
			time.Sleep(500 * time.Millisecond)
		}
		a.ipcServer.Stop()
		log.Info().Msg("IPC server stopped")
	}

	// Stop email sync scheduler
	if a.syncScheduler != nil {
		a.syncScheduler.Stop()
		log.Info().Msg("Email sync scheduler stopped")
	}

	// Stop IDLE manager
	if a.idleManager != nil {
		a.idleManager.Stop()
		log.Info().Msg("IDLE manager stopped")
	}

	// Stop sleep/wake monitor
	if a.sleepWakeMonitor != nil {
		a.sleepWakeMonitor.Stop()
		log.Info().Msg("Sleep/wake monitor stopped")
	}

	// Stop theme monitor
	if a.themeMonitor != nil {
		a.themeMonitor.Stop()
		log.Info().Msg("Theme monitor stopped")
	}

	// Stop notification listener
	if a.notifier != nil {
		a.notifier.Stop()
		log.Info().Msg("Notification listener stopped")
	}

	// Stop CardDAV scheduler
	if a.carddavScheduler != nil {
		a.carddavScheduler.Stop()
		log.Info().Msg("CardDAV scheduler stopped")
	}

	// Close all IMAP connections
	if a.imapPool != nil {
		a.imapPool.CloseAll()
		log.Info().Msg("IMAP connections closed")
	}

	if a.db != nil {
		a.db.Close()
		log.Info().Msg("Database closed")
	}

	log.Info().Msg("Aerion shutdown complete")
}

// updateDBConnectionPool scales the database connection pool based on account count.
// This should be called at startup and whenever accounts are added or removed.
func (a *App) updateDBConnectionPool() {
	accounts, err := a.accountStore.List()
	if err != nil {
		// On error, use a reasonable default
		a.db.UpdateIdleConns(0)
		return
	}
	a.db.UpdateIdleConns(len(accounts))
}

// getIMAPCredentials returns IMAP credentials for an account
// Handles both password and OAuth2 authentication
func (a *App) getIMAPCredentials(accountID string) (*imap.ClientConfig, error) {
	log := logging.WithComponent("app.credentials")

	acc, err := a.accountStore.Get(accountID)
	if err != nil {
		log.Error().Err(err).Str("accountID", accountID).Msg("Failed to get account")
		return nil, err
	}
	if acc == nil {
		log.Error().Str("accountID", accountID).Msg("Account not found")
		return nil, fmt.Errorf("account not found: %s", accountID)
	}

	log.Debug().
		Str("accountID", accountID).
		Str("email", acc.Email).
		Str("authType", string(acc.AuthType)).
		Str("imapHost", acc.IMAPHost).
		Msg("Getting IMAP credentials")

	config := imap.DefaultConfig()
	config.Host = acc.IMAPHost
	config.Port = acc.IMAPPort
	config.Security = imap.SecurityType(acc.IMAPSecurity)
	config.Username = acc.Username
	config.TLSConfig = certificate.BuildTLSConfig(acc.IMAPHost, a.certStore)

	// Handle authentication based on auth type
	if acc.AuthType == account.AuthOAuth2 {
		log.Debug().Str("accountID", accountID).Msg("Using OAuth2 authentication")
		// Get valid OAuth token (refreshing if needed)
		tokens, err := a.getValidOAuthToken(accountID)
		if err != nil {
			log.Error().Err(err).Str("accountID", accountID).Msg("Failed to get OAuth token")
			return nil, fmt.Errorf("failed to get OAuth token: %w", err)
		}
		log.Debug().
			Str("accountID", accountID).
			Time("expiresAt", tokens.ExpiresAt).
			Int("tokenLen", len(tokens.AccessToken)).
			Msg("OAuth token retrieved successfully")
		config.AuthType = imap.AuthTypeOAuth2
		config.AccessToken = tokens.AccessToken
	} else {
		log.Debug().Str("accountID", accountID).Msg("Using password authentication")
		// Default to password authentication
		password, err := a.credStore.GetPassword(accountID)
		if err != nil {
			log.Error().Err(err).Str("accountID", accountID).Msg("Failed to get password")
			return nil, fmt.Errorf("failed to get password: %w", err)
		}
		config.AuthType = imap.AuthTypePassword
		config.Password = password
	}

	log.Debug().
		Str("accountID", accountID).
		Str("authType", string(config.AuthType)).
		Msg("IMAP credentials prepared")

	return &config, nil
}

// getValidOAuthToken returns a valid OAuth token, refreshing if needed
// If refresh fails, emits an event for the frontend to prompt re-authorization
func (a *App) getValidOAuthToken(accountID string) (*credentials.OAuthTokens, error) {
	log := logging.WithComponent("app")

	tokens, err := a.credStore.GetOAuthTokens(accountID)
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
		newTokenResp, err := a.oauth2Manager.RefreshToken(tokens.Provider, tokens.RefreshToken)
		if err != nil {
			log.Error().Err(err).
				Str("account_id", accountID).
				Msg("OAuth token refresh failed")

			// Emit event for frontend to prompt re-authorization
			wailsRuntime.EventsEmit(a.ctx, "oauth:reauth-required", map[string]interface{}{
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

		if err := a.credStore.SetOAuthTokens(accountID, tokens); err != nil {
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

// GetContext returns the app context
func (a *App) GetContext() context.Context {
	return a.ctx
}

// getValidContactSourceOAuthToken returns a valid OAuth token for a standalone contact source
func (a *App) getValidContactSourceOAuthToken(sourceID string) (string, error) {
	log := logging.WithComponent("app")

	tokens, err := a.credStore.GetContactSourceOAuthTokens(sourceID)
	if err != nil {
		return "", fmt.Errorf("failed to get contact source OAuth tokens: %w", err)
	}

	// Check if token expires within 5 minutes
	if tokens.IsExpiringSoon(5 * time.Minute) {
		log.Debug().
			Str("source_id", sourceID).
			Time("expires_at", tokens.ExpiresAt).
			Msg("Contact source OAuth token expiring soon, refreshing")

		// Refresh the token
		newTokenResp, err := a.oauth2Manager.RefreshToken(tokens.Provider, tokens.RefreshToken)
		if err != nil {
			log.Error().Err(err).
				Str("source_id", sourceID).
				Msg("Contact source OAuth token refresh failed")

			// Emit event for frontend to prompt re-authorization
			wailsRuntime.EventsEmit(a.ctx, "contact-source:reauth-required", map[string]interface{}{
				"sourceId": sourceID,
				"provider": tokens.Provider,
				"error":    err.Error(),
			})

			return "", fmt.Errorf("contact source OAuth token refresh failed: %w", err)
		}

		// Calculate new expiry time
		expiresAt := time.Now().Add(time.Duration(newTokenResp.ExpiresIn) * time.Second)

		// Update tokens in store
		tokens.AccessToken = newTokenResp.AccessToken
		tokens.ExpiresAt = expiresAt
		if newTokenResp.RefreshToken != "" {
			tokens.RefreshToken = newTokenResp.RefreshToken
		}

		if err := a.credStore.SetContactSourceOAuthTokens(sourceID, tokens); err != nil {
			log.Warn().Err(err).Msg("Failed to save refreshed contact source OAuth tokens")
		}

		log.Info().
			Str("source_id", sourceID).
			Time("new_expires_at", expiresAt).
			Msg("Contact source OAuth token refreshed successfully")
	}

	return tokens.AccessToken, nil
}

// OpenURL opens a URL in the system browser with proper shell escaping
// This bypasses Wails' BrowserOpenURL which has strict validation against shell metacharacters
func (a *App) OpenURL(url string) error {
	log := logging.WithComponent("app")
	log.Debug().Str("url", url).Msg("Opening URL in system browser")

	// Validate URL and check protocol for security
	// This prevents file:// URLs and other potentially dangerous schemes
	if url == "" {
		return fmt.Errorf("empty URL")
	}

	// Allow common safe protocols
	// Note: We're being permissive here to allow legitimate email links
	// The main security comes from using exec.Command properly
	if !isAllowedProtocol(url) {
		log.Warn().Str("url", url).Msg("Rejecting URL with disallowed protocol")
		return fmt.Errorf("URL protocol not allowed for security reasons")
	}

	var cmd *exec.Cmd

	// Determine the command based on the operating system
	switch runtime.GOOS {
	case "linux":
		// Use xdg-open on Linux
		// exec.Command properly escapes the URL argument, preventing shell injection
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		// Use open on macOS
		cmd = exec.Command("open", url)
	case "windows":
		// Use cmd /c start on Windows
		// Note: Using cmd.exe with proper escaping
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	// Start the command without waiting for it to complete
	// Browser opening should be async - we don't need to wait
	if err := cmd.Start(); err != nil {
		log.Error().Err(err).Str("url", url).Msg("Failed to open URL in browser")
		return fmt.Errorf("failed to open URL: %w", err)
	}

	log.Debug().Str("url", url).Msg("Successfully started browser process")
	return nil
}

// isAllowedProtocol checks if a URL uses an allowed protocol
// Prevents file:// URLs and other potentially dangerous schemes
func isAllowedProtocol(url string) bool {
	// Common safe protocols for an email client
	allowedPrefixes := []string{
		"http://",
		"https://",
		"mailto:",
		// Note: We could add more if needed, but being conservative
	}

	for _, prefix := range allowedPrefixes {
		if len(url) >= len(prefix) && url[:len(prefix)] == prefix {
			return true
		}
	}

	return false
}
