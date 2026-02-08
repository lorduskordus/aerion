// Package oauth2 provides OAuth2 authentication for email providers
package oauth2

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
)

// Build-time variables injected via ldflags
// These are set during compilation using:
//
//	go build -ldflags "-X 'github.com/hkdb/aerion/internal/oauth2.GoogleClientID=xxx'"
//
// See Makefile for the complete build command.
// If ldflags are not set, credentials are loaded from the aerion-creds shim binary.
var (
	// GoogleClientID is the OAuth2 client ID for Google/Gmail
	GoogleClientID string

	// GoogleClientSecret is the OAuth2 client secret for Google/Gmail
	GoogleClientSecret string

	// MicrosoftClientID is the OAuth2 client ID for Microsoft/Outlook
	MicrosoftClientID string
)

func init() {
	if GoogleClientID != "" {
		return
	}
	loadFromShim()
}

func loadFromShim() {
	// Search for the shim binary in known locations
	paths := []string{
		"/app/lib/aerion/aerion-creds", // Flatpak
	}

	// Also check next to the main binary
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), "aerion-creds"))
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		out, err := exec.Command(p).Output()
		if err != nil {
			continue
		}
		var creds map[string]string
		if err := json.Unmarshal(out, &creds); err != nil {
			continue
		}
		GoogleClientID = creds["google_client_id"]
		GoogleClientSecret = creds["google_client_secret"]
		MicrosoftClientID = creds["microsoft_client_id"]
		return
	}
}

// IsGoogleConfigured returns true if Google OAuth credentials are available
func IsGoogleConfigured() bool {
	return GoogleClientID != ""
}

// IsMicrosoftConfigured returns true if Microsoft OAuth credentials are available
func IsMicrosoftConfigured() bool {
	return MicrosoftClientID != ""
}

// IsProviderConfigured returns true if the specified provider has OAuth credentials
func IsProviderConfigured(provider string) bool {
	switch provider {
	case "google":
		return IsGoogleConfigured()
	case "microsoft":
		return IsMicrosoftConfigured()
	default:
		return false
	}
}
