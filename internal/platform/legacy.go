// Legacy data-folder migration.
//
// TODO: remove this file in a later version, once installs that predate
// the Daak rename have had a chance to upgrade. Everything in this file
// exists only to move pre-rename ("aerion") data folders to the new
// ("daak") locations on first run. No other file may reference the old
// folder names.

package platform

import (
	"os"
	"runtime"

	"github.com/absonsnet/daak/internal/logging"
)

// Legacy folder names from before the rename. Linux used the lowercase
// name; macOS and Windows used the display name.
const (
	legacyLinuxFolderName   = "aerion"
	legacyDisplayFolderName = "Aerion"
)

// legacyPaths returns the pre-rename locations of the Config/Data/Cache
// folders, computed from the same environment as the new paths so the two
// differ only in the leaf folder name.
func legacyPaths() (*Paths, error) {
	switch runtime.GOOS {
	case "linux":
		return getLinuxPaths(legacyLinuxFolderName)
	case "darwin":
		return getDarwinPaths(legacyDisplayFolderName)
	case "windows":
		return getWindowsPaths(legacyDisplayFolderName)
	default:
		return getLinuxPaths(legacyLinuxFolderName)
	}
}

// migrateLegacyDataDirs moves each pre-rename data folder to its new
// location. It runs from GetPaths, before paths are first used, and is
// idempotent and safe:
//
//   - old folder exists and new folder does not → rename the directory
//     (the database and everything inside it moves along).
//   - new folder already exists → use it; the old folder is left
//     untouched, never deleted or overwritten.
//   - neither exists → nothing to do; EnsureDirectories creates the new
//     folders for a fresh install.
//
// A failed rename is logged and startup continues with the new (empty)
// folders; the old folder stays where it is so no data is lost silently.
func migrateLegacyDataDirs(newPaths *Paths) {
	oldPaths, err := legacyPaths()
	if err != nil {
		return
	}
	log := logging.WithComponent("platform")
	seen := map[string]bool{}
	for _, pair := range [][2]string{
		{oldPaths.Config, newPaths.Config},
		{oldPaths.Data, newPaths.Data},
		{oldPaths.Cache, newPaths.Cache},
	} {
		oldPath, newPath := pair[0], pair[1]
		if oldPath == newPath || seen[oldPath] {
			continue
		}
		seen[oldPath] = true
		moved, err := migrateLegacyDir(oldPath, newPath)
		switch {
		case err != nil:
			log.Warn().Err(err).Str("old", oldPath).Str("new", newPath).
				Msg("Legacy data folder could not be moved; using the new location, old data left in place")
		case moved:
			log.Info().Str("old", oldPath).Str("new", newPath).
				Msg("Migrated legacy data folder to the new location")
		default:
			// New folder already existed (or neither did): old folder
			// left untouched, nothing to report.
		}
	}
}

// migrateLegacyDir moves one pre-rename folder to its new location per
// the rules in migrateLegacyDataDirs. It reports whether it moved
// anything. Factored out so tests can exercise it on temp dirs without
// touching real data folders.
func migrateLegacyDir(oldPath, newPath string) (bool, error) {
	if oldPath == newPath {
		return false, nil
	}
	oldInfo, err := os.Stat(oldPath)
	if err != nil || !oldInfo.IsDir() {
		// No old folder (or not a directory): nothing to migrate.
		return false, nil
	}
	if _, err := os.Stat(newPath); err == nil {
		// New folder already exists: never overwrite it, and never
		// delete the old folder.
		return false, nil
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return false, err
	}
	return true, nil
}
