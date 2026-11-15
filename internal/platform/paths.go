// Package platform provides platform-specific functionality
package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// linuxFolderName is the lowercase app name used for Linux data folders.
func linuxFolderName() string {
	return strings.ToLower(AppName)
}

// Paths holds the application data paths
type Paths struct {
	Config string // Configuration files (config.toml, accounts/)
	Data   string // Persistent data (databases, keys, attachments)
	Cache  string // Cached data (can be deleted)
}

// GetPaths returns platform-specific paths for the application.
// It also runs the one-time legacy data-folder migration (see legacy.go)
// before the paths are first used, so an upgraded install keeps its data.
func GetPaths() (*Paths, error) {
	var paths *Paths
	var err error
	switch runtime.GOOS {
	case "linux":
		paths, err = getLinuxPaths(linuxFolderName())
	case "darwin":
		paths, err = getDarwinPaths(AppName)
	case "windows":
		paths, err = getWindowsPaths(AppName)
	default:
		// Fallback to Linux-style paths
		paths, err = getLinuxPaths(linuxFolderName())
	}
	if err != nil {
		return nil, err
	}
	migrateLegacyDataDirs(paths)
	return paths, nil
}

// getLinuxPaths returns XDG-compliant paths for Linux
func getLinuxPaths(folderName string) (*Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// XDG Base Directory Specification
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}

	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}

	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		cacheHome = filepath.Join(home, ".cache")
	}

	return &Paths{
		Config: filepath.Join(configHome, folderName),
		Data:   filepath.Join(dataHome, folderName),
		Cache:  filepath.Join(cacheHome, folderName),
	}, nil
}

// getDarwinPaths returns macOS-style paths
func getDarwinPaths(folderName string) (*Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	appSupport := filepath.Join(home, "Library", "Application Support", folderName)
	caches := filepath.Join(home, "Library", "Caches", folderName)

	return &Paths{
		Config: appSupport,
		Data:   appSupport,
		Cache:  caches,
	}, nil
}

// getWindowsPaths returns Windows-style paths
func getWindowsPaths(folderName string) (*Paths, error) {
	// APPDATA is for roaming data (synced across machines)
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		appData = filepath.Join(home, "AppData", "Roaming")
	}

	// LOCALAPPDATA is for local data (not synced)
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		localAppData = filepath.Join(home, "AppData", "Local")
	}

	return &Paths{
		Config: filepath.Join(appData, folderName),
		Data:   filepath.Join(appData, folderName),
		Cache:  filepath.Join(localAppData, folderName, "Cache"),
	}, nil
}

// IsFlatpak returns true if the application is running inside a Flatpak sandbox.
func IsFlatpak() bool {
	return os.Getenv("FLATPAK_ID") != ""
}

// FlatpakHasWritableHome reports whether the Flatpak sandbox has writable
// access to the user's home directory (or the whole host), e.g. granted via
// `flatpak override --user --filesystem=home`. Parsed from the sandbox's
// /.flatpak-info — the canonical manifest of granted permissions ([Context]
// section, `filesystems=` list, entries with optional :ro/:rw/:create
// suffixes). Read-only grants don't count. Always false outside Flatpak
// (the file doesn't exist).
func FlatpakHasWritableHome() bool {
	data, err := os.ReadFile("/.flatpak-info")
	if err != nil {
		return false
	}
	inContext := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inContext = line == "[Context]"
			continue
		}
		if !inContext || !strings.HasPrefix(line, "filesystems=") {
			continue
		}
		for _, fs := range strings.Split(strings.TrimPrefix(line, "filesystems="), ";") {
			base, suffix, _ := strings.Cut(fs, ":")
			if suffix == "ro" {
				continue
			}
			if base == "home" || base == "host" {
				return true
			}
		}
	}
	return false
}

// EnsureDirectories creates all necessary directories if they don't exist
func (p *Paths) EnsureDirectories() error {
	dirs := []string{
		p.Config,
		filepath.Join(p.Config, "accounts"),
		p.Data,
		filepath.Join(p.Data, "search"),
		filepath.Join(p.Data, "attachments"),
		filepath.Join(p.Data, "keys"),
		filepath.Join(p.Data, "extensions"),
		p.Cache,
		filepath.Join(p.Cache, "avatars"),
		filepath.Join(p.Cache, "bodies"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}

	return nil
}

// DatabasePath returns the path to the main database
func (p *Paths) DatabasePath() string {
	return filepath.Join(p.Data, linuxFolderName()+".db")
}

// ContactsDatabasePath returns the path to the contacts database
func (p *Paths) ContactsDatabasePath() string {
	return filepath.Join(p.Data, "contacts.db")
}

// SearchIndexPath returns the path to the search index directory for an account
func (p *Paths) SearchIndexPath(accountID string) string {
	return filepath.Join(p.Data, "search", accountID)
}

// KeyringPath returns the path to the PGP keyring directory
func (p *Paths) KeyringPath() string {
	return filepath.Join(p.Data, "keys")
}

// ConfigFilePath returns the path to the main config file
func (p *Paths) ConfigFilePath() string {
	return filepath.Join(p.Config, "config.toml")
}

// AttachmentsPath returns the path to the attachments directory
func (p *Paths) AttachmentsPath() string {
	return filepath.Join(p.Data, "attachments")
}

// ExtensionsDir returns the root directory containing per-extension SQLite
// databases at <ExtensionsDir>/<name>/data.db. Each extension owns its own
// subdirectory; cross-extension data access goes through the v1 Core API.
func (p *Paths) ExtensionsDir() string {
	return filepath.Join(p.Data, "extensions")
}
