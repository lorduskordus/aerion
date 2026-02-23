package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	goImap "github.com/emersion/go-imap/v2"
	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/certificate"
	"github.com/hkdb/aerion/internal/folder"
	"github.com/hkdb/aerion/internal/imap"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/message"
	"github.com/hkdb/aerion/internal/smtp"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ComposerAttachment represents an attachment in the compose window
type ComposerAttachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	Data        string `json:"data"` // Base64 encoded
}

// ============================================================================
// Compose API - Exposed to frontend via Wails bindings
// ============================================================================

// SendMessage sends an email via SMTP.
// The message is composed in the frontend and sent to the backend.
func (a *App) SendMessage(accountID string, msg smtp.ComposeMessage) error {
	log := logging.WithComponent("app")

	log.Info().
		Str("accountID", accountID).
		Str("from", msg.From.Address).
		Int("toCount", len(msg.To)).
		Str("subject", msg.Subject).
		Msg("Sending message")

	// Get account
	acc, err := a.accountStore.Get(accountID)
	if err != nil {
		return fmt.Errorf("failed to get account: %w", err)
	}
	if acc == nil {
		return fmt.Errorf("account not found: %s", accountID)
	}

	// Build RFC822 message
	rawMsg, err := msg.ToRFC822()
	if err != nil {
		return fmt.Errorf("failed to build message: %w", err)
	}

	// The sender's email determines which cert/key to use
	fromEmail := msg.From.Address

	// S/MIME signing (if configured for this account/message)
	if a.shouldSignMessage(accountID, msg.SignMessage) {
		signedMsg, signErr := a.smimeSigner.SignMessage(accountID, fromEmail, rawMsg)
		if signErr != nil {
			return fmt.Errorf("failed to sign message: %w", signErr)
		}
		rawMsg = signedMsg
		log.Info().Str("accountID", accountID).Msg("Message signed with S/MIME")
	}

	// S/MIME encryption (if configured for this account/message) — sign-then-encrypt per RFC 5751
	if a.shouldEncryptMessage(accountID, msg.EncryptMessage) {
		encryptedMsg, encErr := a.smimeEncryptor.EncryptMessage(accountID, fromEmail, msg.AllRecipients(), rawMsg)
		if encErr != nil {
			return fmt.Errorf("failed to encrypt message: %w", encErr)
		}
		rawMsg = encryptedMsg
		log.Info().Str("accountID", accountID).Msg("Message encrypted with S/MIME")
	}

	// PGP signing (mutually exclusive with S/MIME — only if S/MIME sign was not applied)
	if !msg.SignMessage && a.shouldPGPSignMessage(accountID, msg.PGPSignMessage) {
		signedMsg, signErr := a.pgpSigner.SignMessage(accountID, fromEmail, rawMsg)
		if signErr != nil {
			return fmt.Errorf("failed to PGP sign message: %w", signErr)
		}
		rawMsg = signedMsg
		log.Info().Str("accountID", accountID).Msg("Message signed with PGP")
	}

	// PGP encryption (mutually exclusive with S/MIME — only if S/MIME encrypt was not applied)
	if !msg.EncryptMessage && a.shouldPGPEncryptMessage(accountID, msg.PGPEncryptMessage) {
		encryptedMsg, encErr := a.pgpEncryptor.EncryptMessage(accountID, fromEmail, msg.AllRecipients(), rawMsg)
		if encErr != nil {
			return fmt.Errorf("failed to PGP encrypt message: %w", encErr)
		}
		rawMsg = encryptedMsg
		log.Info().Str("accountID", accountID).Msg("Message encrypted with PGP")
	}

	// Create SMTP client config
	smtpConfig := smtp.DefaultConfig()
	smtpConfig.Host = acc.SMTPHost
	smtpConfig.Port = acc.SMTPPort
	smtpConfig.Security = smtp.SecurityType(acc.SMTPSecurity)
	smtpConfig.Username = acc.Username
	smtpConfig.TLSConfig = certificate.BuildTLSConfig(acc.SMTPHost, a.certStore)

	// Handle authentication based on auth type
	if acc.AuthType == account.AuthOAuth2 {
		// Get valid OAuth token (refreshing if needed)
		tokens, err := a.getValidOAuthToken(accountID)
		if err != nil {
			return fmt.Errorf("failed to get OAuth token: %w", err)
		}
		smtpConfig.AuthType = smtp.AuthTypeOAuth2
		smtpConfig.AccessToken = tokens.AccessToken
	} else {
		// Default to password authentication
		password, err := a.credStore.GetPassword(accountID)
		if err != nil {
			return fmt.Errorf("failed to get password: %w", err)
		}
		smtpConfig.AuthType = smtp.AuthTypePassword
		smtpConfig.Password = password
	}

	client := smtp.NewClient(smtpConfig)

	// Connect
	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer client.Close()

	// Login
	if err := client.Login(); err != nil {
		return fmt.Errorf("failed to login to SMTP server: %w", err)
	}

	// Send
	recipients := msg.AllRecipients()
	if len(recipients) == 0 {
		return fmt.Errorf("no recipients specified")
	}

	if err := client.SendMail(msg.From.Address, recipients, rawMsg); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	// Save to Sent folder (using IMAP APPEND) if provider doesn't auto-save
	if !providerAutoSavesSentMail(acc.IMAPHost) {
		log.Debug().Str("host", acc.IMAPHost).Msg("Provider doesn't auto-save, using IMAP APPEND")
		if err := a.saveToSentFolder(accountID, acc, rawMsg); err != nil {
			log.Warn().Err(err).Msg("Failed to save message to Sent folder")
			// Don't fail the send operation if saving fails
		}
	} else {
		log.Debug().Str("host", acc.IMAPHost).Msg("Provider auto-saves sent mail, skipping manual save")
	}

	// Add recipients to local contacts
	for _, to := range msg.To {
		a.contactStore.AddOrUpdate(to.Address, to.Name)
	}
	for _, cc := range msg.Cc {
		a.contactStore.AddOrUpdate(cc.Address, cc.Name)
	}

	// Sync sent folder to get the sent message
	go a.syncSentFolder(accountID)

	log.Info().Str("accountID", accountID).Msg("Message sent successfully")
	return nil
}

// syncSentFolder syncs the Sent folder for an account after sending a message
func (a *App) syncSentFolder(accountID string) error {
	log := logging.WithComponent("app")

	sentFolder, err := a.GetSpecialFolder(accountID, folder.TypeSent)
	if err != nil || sentFolder == nil {
		log.Warn().Str("accountID", accountID).Msg("Could not find Sent folder for sync")
		return nil
	}

	// Get account to determine sync period
	acc, _ := a.accountStore.Get(accountID)
	syncPeriodDays := 30 // default
	if acc != nil {
		syncPeriodDays = acc.SyncPeriodDays
	}

	// Emit syncing event
	wailsRuntime.EventsEmit(a.ctx, "folder:syncing", map[string]interface{}{
		"accountId": accountID,
		"folderId":  sentFolder.ID,
	})

	if err := a.syncEngine.SyncMessages(a.ctx, accountID, sentFolder.ID, syncPeriodDays); err != nil {
		log.Warn().Err(err).Str("folderID", sentFolder.ID).Msg("Failed to sync Sent folder")
	}

	// Also fetch bodies
	if err := a.syncEngine.FetchBodiesInBackground(a.ctx, accountID, sentFolder.ID, syncPeriodDays); err != nil {
		log.Warn().Err(err).Str("folderID", sentFolder.ID).Msg("Failed to fetch bodies for Sent folder")
	}

	// Emit synced event
	wailsRuntime.EventsEmit(a.ctx, "folder:synced", map[string]interface{}{
		"accountId": accountID,
		"folderId":  sentFolder.ID,
	})

	return nil
}

// saveToSentFolder appends the sent message to the Sent folder via IMAP
func (a *App) saveToSentFolder(accountID string, acc *account.Account, rawMsg []byte) error {
	log := logging.WithComponent("app")

	// Get the Sent folder path (from account mapping or auto-detected)
	sentFolder, err := a.GetSpecialFolder(accountID, folder.TypeSent)
	if err != nil || sentFolder == nil {
		// Fall back to account's configured sent folder path
		if acc.SentFolderPath == "" {
			return fmt.Errorf("no Sent folder configured or detected")
		}
	}

	sentPath := acc.SentFolderPath
	if sentPath == "" && sentFolder != nil {
		sentPath = sentFolder.Path
	}

	log.Debug().
		Str("account_id", accountID).
		Str("sent_path", sentPath).
		Msg("Saving sent message to folder via IMAP APPEND")

	// Create IMAP client
	clientConfig := imap.DefaultConfig()
	clientConfig.Host = acc.IMAPHost
	clientConfig.Port = acc.IMAPPort
	clientConfig.Security = imap.SecurityType(acc.IMAPSecurity)
	clientConfig.Username = acc.Username
	clientConfig.TLSConfig = certificate.BuildTLSConfig(acc.IMAPHost, a.certStore)

	// Handle authentication based on auth type
	if acc.AuthType == account.AuthOAuth2 {
		tokens, err := a.getValidOAuthToken(accountID)
		if err != nil {
			return fmt.Errorf("failed to get OAuth token: %w", err)
		}
		clientConfig.AuthType = imap.AuthTypeOAuth2
		clientConfig.AccessToken = tokens.AccessToken
	} else {
		password, err := a.credStore.GetPassword(accountID)
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
		Str("account_id", accountID).
		Str("sent_path", sentPath).
		Msg("Message saved to Sent folder")

	return nil
}

// PrepareReply prepares a reply message structure from an existing message.
// mode can be "reply", "reply-all", or "forward"
func (a *App) PrepareReply(messageID, mode string) (*smtp.ComposeMessage, error) {
	log := logging.WithComponent("app")
	log.Debug().Str("messageID", messageID).Str("mode", mode).Msg("Preparing reply message")

	// Get the original message
	msg, err := a.messageStore.Get(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}

	// Get account and identities
	identities, err := a.accountStore.GetIdentities(msg.AccountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get identities: %w", err)
	}

	// Find the default identity or first identity
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
	if fromIdentity == nil {
		acc, _ := a.accountStore.Get(msg.AccountID)
		if acc != nil {
			fromIdentity = &account.Identity{
				Email: acc.Email,
				Name:  acc.Name,
			}
		}
	}

	// Build the From address
	from := smtp.Address{}
	if fromIdentity != nil {
		from = smtp.Address{Name: fromIdentity.Name, Address: fromIdentity.Email}
	}

	// Build subject with Re: or Fwd: prefix
	subject := msg.Subject
	switch mode {
	case "forward":
		if !strings.HasPrefix(strings.ToLower(subject), "fwd:") && !strings.HasPrefix(strings.ToLower(subject), "fw:") {
			subject = "Fwd: " + subject
		}
	case "reply", "reply-all":
		if !strings.HasPrefix(strings.ToLower(subject), "re:") {
			subject = "Re: " + subject
		}
	}

	// Build the To and Cc lists based on mode
	var to, cc []smtp.Address
	selfEmails := make(map[string]bool)
	for _, id := range identities {
		selfEmails[strings.ToLower(strings.TrimSpace(id.Email))] = true
	}

	originalFrom := []smtp.Address{{Name: msg.FromName, Address: strings.TrimSpace(msg.FromEmail)}}

	switch mode {
	case "reply":
		// Reply to sender only
		to = filterSelfAddresses(originalFrom, selfEmails)

		// Defensive fix: if reply resulted in no recipients (replying to self),
		// include the original sender anyway
		if len(to) == 0 && len(originalFrom) > 0 {
			to = originalFrom
		}
	case "reply-all":
		// Reply to sender
		to = filterSelfAddresses(originalFrom, selfEmails)
		// Include original To recipients (excluding self)
		originalTo := parseAddressList(msg.ToList)
		to = append(to, filterSelfAddresses(originalTo, selfEmails)...)
		// Include original Cc recipients (excluding self and duplicates from To)
		originalCc := parseAddressList(msg.CcList)
		toSet := make(map[string]bool)
		for _, addr := range to {
			toSet[strings.ToLower(strings.TrimSpace(addr.Address))] = true
		}
		for _, addr := range filterSelfAddresses(originalCc, selfEmails) {
			if !toSet[strings.ToLower(strings.TrimSpace(addr.Address))] {
				cc = append(cc, addr)
			}
		}

		// Defensive fix: if reply-all resulted in no recipients at all,
		// include the original sender even if it matches a self email
		// (this can happen when replying to your own sent messages)
		if len(to) == 0 && len(cc) == 0 && len(originalFrom) > 0 {
			to = originalFrom
		}
	case "forward":
		// Leave To empty for user to fill in
	}

	// Build the quoted body
	dateStr := msg.Date.Format("Mon, Jan 2 2006 at 3:04:05 PM MST")
	sender := msg.FromEmail
	if msg.FromName != "" {
		sender = msg.FromName + " <" + msg.FromEmail + ">"
	}

	var htmlBody, textBody string
	if mode == "forward" {
		// Forward format
		htmlBody = fmt.Sprintf("<p></p><p></p><p>---------- Forwarded message ----------<br>From: %s<br>Subject: %s<br>Date: %s<br>To: %s</p><p></p>%s",
			escapeHTML(sender), escapeHTML(msg.Subject), escapeHTML(dateStr), escapeHTML(msg.ToList), msg.BodyHTML)
		textBody = fmt.Sprintf("\n\n---------- Forwarded message ----------\nFrom: %s\nSubject: %s\nDate: %s\nTo: %s\n\n%s",
			sender, msg.Subject, dateStr, msg.ToList, msg.BodyText)
	} else {
		// Reply format
		citation := fmt.Sprintf("On %s, %s wrote:", dateStr, sender)
		htmlBody = fmt.Sprintf("<p></p><p></p><p>%s</p><blockquote type=\"cite\">%s</blockquote>", escapeHTML(citation), msg.BodyHTML)
		textBody = fmt.Sprintf("\n\n%s\n%s", citation, quoteText(msg.BodyText))
	}

	// Build References header per RFC 5322:
	// References = parent's References + parent's Message-ID
	var refs []string
	if msg.References != "" {
		// References are stored as a JSON array in the DB
		json.Unmarshal([]byte(msg.References), &refs)
	}
	if msg.MessageID != "" {
		refs = append(refs, ensureAngleBrackets(msg.MessageID))
	}

	return &smtp.ComposeMessage{
		From:       from,
		To:         to,
		Cc:         cc,
		Subject:    subject,
		HTMLBody:   htmlBody,
		TextBody:   textBody,
		InReplyTo:  ensureAngleBrackets(msg.MessageID),
		References: refs,
	}, nil
}

// TestSMTPConnection tests SMTP connection settings
func (a *App) TestSMTPConnection(host string, port int, security, username, password string) error {
	log := logging.WithComponent("app")

	// Map security string to type
	var securityType smtp.SecurityType
	switch security {
	case "none":
		securityType = smtp.SecurityNone
	case "starttls":
		securityType = smtp.SecurityStartTLS
	case "tls":
		securityType = smtp.SecurityTLS
	default:
		securityType = smtp.SecurityStartTLS
	}

	// Create client config
	config := smtp.DefaultConfig()
	config.Host = host
	config.Port = port
	config.Security = securityType
	config.Username = username
	config.Password = password
	config.AuthType = smtp.AuthTypePassword
	config.TLSConfig = certificate.BuildTLSConfig(host, a.certStore)

	client := smtp.NewClient(config)

	// Test connection
	if err := client.Connect(); err != nil {
		log.Error().Err(err).Msg("SMTP connection test failed")
		return fmt.Errorf("connection failed: %w", err)
	}
	defer client.Close()

	// Test authentication
	if err := client.Login(); err != nil {
		log.Error().Err(err).Msg("SMTP login test failed")
		return fmt.Errorf("authentication failed: %w", err)
	}

	log.Info().Str("host", host).Int("port", port).Msg("SMTP connection test successful")
	return nil
}

// PickAttachmentFiles opens a file picker dialog and returns the selected files as attachments
func (a *App) PickAttachmentFiles() ([]ComposerAttachment, error) {
	log := logging.WithComponent("app")

	// Show multi-file picker dialog
	files, err := wailsRuntime.OpenMultipleFilesDialog(a.ctx, wailsRuntime.OpenDialogOptions{
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
		att, err := a.readFileAsAttachment(filePath)
		if err != nil {
			log.Warn().Err(err).Str("path", filePath).Msg("Failed to read file as attachment")
			continue
		}
		attachments = append(attachments, *att)
	}

	log.Info().Int("count", len(attachments)).Msg("Files picked for attachment")
	return attachments, nil
}

// ReadFileAsAttachment reads a file and creates a ComposerAttachment
func (a *App) ReadFileAsAttachment(filePath string) (*ComposerAttachment, error) {
	return a.readFileAsAttachment(filePath)
}

// readFileAsAttachment is the internal implementation
func (a *App) readFileAsAttachment(filePath string) (*ComposerAttachment, error) {
	log := logging.WithComponent("app")

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
	encoded := encodeBase64(content)

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
// Helper Functions
// ============================================================================

// parseAddressList parses a JSON array of addresses or comma-separated string
func parseAddressList(s string) []smtp.Address {
	if s == "" {
		return nil
	}

	// Try smtp.Address JSON format first (uses "address" field) —
	// this is what addressListToJSON stores for drafts
	var smtpAddrs []smtp.Address
	if err := json.Unmarshal([]byte(s), &smtpAddrs); err == nil {
		// Check if the addresses actually have data (not just zero values)
		if len(smtpAddrs) > 0 && smtpAddrs[0].Address != "" {
			return smtpAddrs
		}
	}

	// Try message.Address JSON format (uses "email" field) —
	// this is what the message store uses
	var msgAddrs []message.Address
	if err := json.Unmarshal([]byte(s), &msgAddrs); err == nil {
		var addrs []smtp.Address
		for _, msgAddr := range msgAddrs {
			addrs = append(addrs, smtp.Address{
				Name:    strings.TrimSpace(msgAddr.Name),
				Address: strings.TrimSpace(msgAddr.Email),
			})
		}
		if len(addrs) > 0 && addrs[0].Address != "" {
			return addrs
		}
	}

	// Try as comma-separated list (legacy format)
	var result []smtp.Address
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// Parse "Name <email>" format
		if strings.Contains(part, "<") {
			start := strings.Index(part, "<")
			end := strings.Index(part, ">")
			if start > 0 && end > start {
				name := strings.TrimSpace(part[:start])
				email := strings.TrimSpace(part[start+1 : end])
				result = append(result, smtp.Address{Name: name, Address: email})
				continue
			}
		}
		result = append(result, smtp.Address{Address: strings.TrimSpace(part)})
	}
	return result
}

// filterSelfAddresses removes the user's own addresses from a list
func filterSelfAddresses(addrs []smtp.Address, selfEmails map[string]bool) []smtp.Address {
	var result []smtp.Address
	for _, addr := range addrs {
		lowerAddr := strings.ToLower(strings.TrimSpace(addr.Address))
		if !selfEmails[lowerAddr] {
			result = append(result, addr)
		}
	}
	return result
}

// escapeHTML escapes special HTML characters
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// ensureAngleBrackets wraps a Message-ID in angle brackets if not already present
func ensureAngleBrackets(msgID string) string {
	if msgID == "" {
		return ""
	}
	msgID = strings.TrimSpace(msgID)
	if !strings.HasPrefix(msgID, "<") {
		msgID = "<" + msgID
	}
	if !strings.HasSuffix(msgID, ">") {
		msgID = msgID + ">"
	}
	return msgID
}

// quoteText adds > prefix to each line for plain text quoting
func quoteText(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = "> " + line
	}
	return strings.Join(lines, "\n")
}

// providerAutoSavesSentMail checks if a mail provider automatically saves sent messages
func providerAutoSavesSentMail(host string) bool {
	host = strings.ToLower(host)
	autoSaveProviders := []string{
		"imap.gmail.com",       // Gmail
		"outlook.office365.com", // Microsoft 365
		"imap-mail.outlook.com", // Outlook.com
	}
	for _, provider := range autoSaveProviders {
		if strings.Contains(host, provider) {
			return true
		}
	}
	return false
}

// addressListToJSON converts an address list to JSON string
func addressListToJSON(addrs []smtp.Address) string {
	if len(addrs) == 0 {
		return ""
	}
	data, _ := json.Marshal(addrs)
	return string(data)
}

// detectContentType returns the MIME type for a file based on extension
func detectContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	// Images
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".bmp":
		return "image/bmp"
	// Documents
	case ".pdf":
		return "application/pdf"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".odt":
		return "application/vnd.oasis.opendocument.text"
	case ".ods":
		return "application/vnd.oasis.opendocument.spreadsheet"
	case ".odp":
		return "application/vnd.oasis.opendocument.presentation"
	// Text
	case ".txt":
		return "text/plain"
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "text/javascript"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".csv":
		return "text/csv"
	case ".md":
		return "text/markdown"
	// Archives
	case ".zip":
		return "application/zip"
	case ".tar":
		return "application/x-tar"
	case ".gz", ".gzip":
		return "application/gzip"
	case ".7z":
		return "application/x-7z-compressed"
	case ".rar":
		return "application/vnd.rar"
	// Audio
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	// Video
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".mkv":
		return "video/x-matroska"
	default:
		return "application/octet-stream"
	}
}

// encodeBase64 encodes bytes to base64 string
func encodeBase64(data []byte) string {
	return fmt.Sprintf("%s",
		func() string {
			encoded := make([]byte, (len(data)+2)/3*4)
			base64Encode(encoded, data)
			return string(encoded[:base64EncodedLen(len(data))])
		}())
}

// base64 encoding table
const base64Table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

func base64Encode(dst, src []byte) {
	for len(src) >= 3 {
		dst[0] = base64Table[src[0]>>2]
		dst[1] = base64Table[(src[0]&0x03)<<4|src[1]>>4]
		dst[2] = base64Table[(src[1]&0x0f)<<2|src[2]>>6]
		dst[3] = base64Table[src[2]&0x3f]
		src = src[3:]
		dst = dst[4:]
	}
	if len(src) > 0 {
		dst[0] = base64Table[src[0]>>2]
		if len(src) == 1 {
			dst[1] = base64Table[(src[0]&0x03)<<4]
			dst[2] = '='
		} else {
			dst[1] = base64Table[(src[0]&0x03)<<4|src[1]>>4]
			dst[2] = base64Table[(src[1]&0x0f)<<2]
		}
		dst[3] = '='
	}
}

func base64EncodedLen(n int) int {
	return (n + 2) / 3 * 4
}
