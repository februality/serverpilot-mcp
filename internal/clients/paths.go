package clients

import (
	"os"
	"path/filepath"
	"runtime"
)

// homePath joins the user's home directory with the given relative parts.
// Panics on systems where home cannot be determined — those are not
// supported deployment targets.
func homePath(parts ...string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		panic("cannot determine user home: " + err.Error())
	}
	return filepath.Join(append([]string{home}, parts...)...)
}

// claudeDesktopConfigPath returns the per-OS Claude Desktop config path.
//
//	macOS:   ~/Library/Application Support/Claude/claude_desktop_config.json
//	Windows: %APPDATA%/Claude/claude_desktop_config.json
//	Linux:   ~/.config/Claude/claude_desktop_config.json
func claudeDesktopConfigPath() string {
	switch runtime.GOOS {
	case "darwin":
		return homePath("Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = homePath("AppData", "Roaming")
		}
		return filepath.Join(appData, "Claude", "claude_desktop_config.json")
	default:
		// Linux + others
		dir, err := os.UserConfigDir()
		if err != nil {
			dir = homePath(".config")
		}
		return filepath.Join(dir, "Claude", "claude_desktop_config.json")
	}
}
