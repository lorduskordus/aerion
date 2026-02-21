package platform

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hkdb/aerion/internal/logging"
)

const (
	autostartFilename = "io.github.hkdb.aerion.desktop"
	desktopEntryTmpl  = `[Desktop Entry]
Type=Application
Name=Aerion
Comment=Aerion Email Client
Exec=%s --start-hidden
Icon=io.github.hkdb.Aerion
Terminal=false
Categories=Network;Email;
X-GNOME-Autostart-enabled=true
`
)

// linuxAutostartManager manages XDG autostart .desktop files.
type linuxAutostartManager struct {
	isFlatpak bool
}

// NewAutostartManager creates a new autostart manager.
func NewAutostartManager() AutostartManager {
	return &linuxAutostartManager{
		isFlatpak: os.Getenv("FLATPAK_ID") != "",
	}
}

// Enable creates the autostart .desktop file.
func (m *linuxAutostartManager) Enable() error {
	log := logging.WithComponent("autostart")

	dir, err := m.autostartDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create autostart directory: %w", err)
	}

	execCmd := m.execCommand()
	content := fmt.Sprintf(desktopEntryTmpl, execCmd)

	path := filepath.Join(dir, autostartFilename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write autostart file: %w", err)
	}

	log.Info().Str("path", path).Msg("Autostart enabled")
	return nil
}

// Disable removes the autostart .desktop file.
func (m *linuxAutostartManager) Disable() error {
	log := logging.WithComponent("autostart")

	dir, err := m.autostartDir()
	if err != nil {
		return err
	}

	path := filepath.Join(dir, autostartFilename)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove autostart file: %w", err)
	}

	log.Info().Str("path", path).Msg("Autostart disabled")
	return nil
}

// IsEnabled checks if the autostart .desktop file exists.
func (m *linuxAutostartManager) IsEnabled() bool {
	dir, err := m.autostartDir()
	if err != nil {
		return false
	}

	path := filepath.Join(dir, autostartFilename)
	_, err = os.Stat(path)
	return err == nil
}

// autostartDir returns the XDG autostart directory.
// For Flatpak: uses the host autostart directory via ~/.config/autostart
// (Flatpak apps have access to $XDG_CONFIG_HOME which maps to the host).
func (m *linuxAutostartManager) autostartDir() (string, error) {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		configDir = filepath.Join(home, ".config")
	}

	// For Flatpak, XDG_CONFIG_HOME points inside the sandbox (~/.var/app/io.github.hkdb.Aerion/config).
	// We need the host autostart dir instead.
	if m.isFlatpak {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		return filepath.Join(home, ".config", "autostart"), nil
	}

	return filepath.Join(configDir, "autostart"), nil
}

// execCommand returns the appropriate Exec= value for the .desktop file.
func (m *linuxAutostartManager) execCommand() string {
	if m.isFlatpak {
		return "flatpak run io.github.hkdb.Aerion"
	}

	// Use the current executable path for non-Flatpak installs
	exe, err := os.Executable()
	if err != nil {
		return "aerion"
	}
	return exe
}
