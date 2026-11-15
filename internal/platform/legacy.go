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
	"path/filepath"
	"runtime"

	"github.com/absonsnet/daak/internal/logging"
)

// Legacy folder names from before the rename. Linux used the lowercase
// name; macOS and Windows used the display name.
const (
	legacyLinuxFolderName   = "aerion"
	legacyDisplayFolderName = "Aerion"
	// legacyDatabaseFileName is the pre-rename main database filename.
	// The new code looks for daak.db (see Paths.DatabasePath), so the
	// file has to be renamed along with its folder — renaming the
	// directory alone leaves the data stranded in aerion.db while the
	// app opens an empty daak.db.
	legacyDatabaseFileName = "aerion.db"
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
	// The database filename changed with the rename too. This runs even
	// when no folder moved: an install whose folder was already moved
	// (or that already had a new folder) can still hold a legacy-named
	// database file, and the app would otherwise open an empty daak.db
	// next to it. See migrateLegacyDatabaseFile.
	migrateLegacyDatabaseFile(newPaths.Data)
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

// migrateLegacyDatabaseFile renames a pre-rename database file inside the
// new data folder to the new filename. Renaming the folder alone is not
// enough: the file inside keeps its old name while DatabasePath() points
// at daak.db, so without this step an upgraded install would open a fresh
// empty database and its mail would look lost.
//
// Rules, in order:
//   - no legacy file → nothing to do.
//   - no new file → rename the legacy file to the new name.
//   - new file exists but is empty (0 bytes, never written) and the legacy
//     file is not → replace the empty file with the legacy one.
//   - new file exists and is not empty → leave both alone and warn; the
//     legacy file stays as an orphan rather than risking data loss.
//
// A failure is logged and startup continues; the legacy file stays where
// it is so no data is lost silently.
func migrateLegacyDatabaseFile(dataDir string) {
	log := logging.WithComponent("platform")
	oldFile := filepath.Join(dataDir, legacyDatabaseFileName)
	newFile := filepath.Join(dataDir, linuxFolderName()+".db")
	if oldFile == newFile {
		return
	}
	oldInfo, err := os.Stat(oldFile)
	if err != nil || oldInfo.IsDir() {
		return
	}
	newInfo, err := os.Stat(newFile)
	if err != nil {
		if os.Rename(oldFile, newFile) != nil {
			log.Warn().Str("old", oldFile).Str("new", newFile).
				Msg("Legacy database file could not be renamed; old data left in place")
			return
		}
		log.Info().Str("old", oldFile).Str("new", newFile).
			Msg("Migrated legacy database file to the new filename")
		return
	}
	if newInfo.IsDir() {
		log.Warn().Str("file", newFile).
			Msg("New database path is a directory; legacy database file left in place")
		return
	}
	if newInfo.Size() == 0 && oldInfo.Size() > 0 {
		// The new file was created empty (e.g. by an earlier launch
		// before this fix); the legacy file holds the real data.
		if err := os.Remove(newFile); err != nil {
			log.Warn().Err(err).Str("file", newFile).
				Msg("Empty new database file could not be removed; legacy database file left in place")
			return
		}
		if err := os.Rename(oldFile, newFile); err != nil {
			log.Warn().Err(err).Str("old", oldFile).Str("new", newFile).
				Msg("Legacy database file could not be renamed; old data left in place")
			return
		}
		log.Info().Str("old", oldFile).Str("new", newFile).
			Msg("Migrated legacy database file to the new filename")
		return
	}
	log.Warn().Str("old", oldFile).Str("new", newFile).
		Msg("Both legacy and new database files exist with data; legacy file left in place")
}
