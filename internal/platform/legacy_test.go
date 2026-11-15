package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMigrateLegacyDir covers the four migration cases on temp dirs —
// never the real data dir.
func TestMigrateLegacyDir(t *testing.T) {
	marker := func(dir string) {
		if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("data"), 0600); err != nil {
			t.Fatalf("write marker: %v", err)
		}
	}

	t.Run("old folder only moves to the new location", func(t *testing.T) {
		root := t.TempDir()
		oldPath := filepath.Join(root, "old")
		newPath := filepath.Join(root, "new")
		if err := os.MkdirAll(oldPath, 0700); err != nil {
			t.Fatal(err)
		}
		marker(oldPath)

		moved, err := migrateLegacyDir(oldPath, newPath)
		if err != nil {
			t.Fatalf("migrateLegacyDir returned error: %v", err)
		}
		if !moved {
			t.Fatal("migrateLegacyDir did not move the old folder")
		}
		if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
			t.Errorf("old folder still exists after migration")
		}
		if _, err := os.Stat(filepath.Join(newPath, "marker.txt")); err != nil {
			t.Errorf("marker file did not move to the new folder: %v", err)
		}
	})

	t.Run("new folder exists leaves both folders untouched", func(t *testing.T) {
		root := t.TempDir()
		oldPath := filepath.Join(root, "old")
		newPath := filepath.Join(root, "new")
		if err := os.MkdirAll(oldPath, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(newPath, 0700); err != nil {
			t.Fatal(err)
		}
		marker(oldPath)
		marker(newPath)

		moved, err := migrateLegacyDir(oldPath, newPath)
		if err != nil {
			t.Fatalf("migrateLegacyDir returned error: %v", err)
		}
		if moved {
			t.Fatal("migrateLegacyDir moved a folder even though the new folder already existed")
		}
		if _, err := os.Stat(filepath.Join(oldPath, "marker.txt")); err != nil {
			t.Errorf("old folder was touched: %v", err)
		}
		if _, err := os.Stat(filepath.Join(newPath, "marker.txt")); err != nil {
			t.Errorf("new folder was touched: %v", err)
		}
	})

	t.Run("neither exists is a no-op", func(t *testing.T) {
		root := t.TempDir()
		oldPath := filepath.Join(root, "old")
		newPath := filepath.Join(root, "new")

		moved, err := migrateLegacyDir(oldPath, newPath)
		if err != nil {
			t.Fatalf("migrateLegacyDir returned error: %v", err)
		}
		if moved {
			t.Fatal("migrateLegacyDir reported a move when neither folder existed")
		}
		if _, err := os.Stat(newPath); !os.IsNotExist(err) {
			t.Errorf("new folder was created by the migration")
		}
	})

	t.Run("both exist leaves both folders untouched", func(t *testing.T) {
		root := t.TempDir()
		oldPath := filepath.Join(root, "old")
		newPath := filepath.Join(root, "new")
		if err := os.MkdirAll(oldPath, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(newPath, 0700); err != nil {
			t.Fatal(err)
		}
		marker(oldPath)
		if err := os.WriteFile(filepath.Join(newPath, "other.txt"), []byte("new"), 0600); err != nil {
			t.Fatal(err)
		}

		moved, err := migrateLegacyDir(oldPath, newPath)
		if err != nil {
			t.Fatalf("migrateLegacyDir returned error: %v", err)
		}
		if moved {
			t.Fatal("migrateLegacyDir moved a folder even though both folders existed")
		}
		if _, err := os.Stat(filepath.Join(oldPath, "marker.txt")); err != nil {
			t.Errorf("old folder was touched: %v", err)
		}
		if _, err := os.Stat(filepath.Join(newPath, "other.txt")); err != nil {
			t.Errorf("new folder was touched: %v", err)
		}
	})
}

// TestMigrateLegacyDatabaseFile covers the database-file rename cases on
// temp dirs — never the real data dir.
func TestMigrateLegacyDatabaseFile(t *testing.T) {
	newName := linuxFolderName() + ".db"
	write := func(dir, name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	read := func(dir, name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(b)
	}

	t.Run("legacy file only is renamed to the new filename", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, legacyDatabaseFileName, "data")

		migrateLegacyDatabaseFile(dir)

		if _, err := os.Stat(filepath.Join(dir, legacyDatabaseFileName)); !os.IsNotExist(err) {
			t.Errorf("legacy database file still exists after migration")
		}
		if got := read(dir, newName); got != "data" {
			t.Errorf("new database file = %q, want %q", got, "data")
		}
	})

	t.Run("empty new file is replaced by the legacy file", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, legacyDatabaseFileName, "data")
		write(dir, newName, "")

		migrateLegacyDatabaseFile(dir)

		if _, err := os.Stat(filepath.Join(dir, legacyDatabaseFileName)); !os.IsNotExist(err) {
			t.Errorf("legacy database file still exists after migration")
		}
		if got := read(dir, newName); got != "data" {
			t.Errorf("new database file = %q, want %q", got, "data")
		}
	})

	t.Run("non-empty new file leaves both files untouched", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, legacyDatabaseFileName, "old")
		write(dir, newName, "new")

		migrateLegacyDatabaseFile(dir)

		if got := read(dir, legacyDatabaseFileName); got != "old" {
			t.Errorf("legacy database file = %q, want %q", got, "old")
		}
		if got := read(dir, newName); got != "new" {
			t.Errorf("new database file = %q, want %q", got, "new")
		}
	})

	t.Run("neither file is a no-op", func(t *testing.T) {
		dir := t.TempDir()

		migrateLegacyDatabaseFile(dir)

		if _, err := os.Stat(filepath.Join(dir, newName)); !os.IsNotExist(err) {
			t.Errorf("new database file was created by the migration")
		}
	})
}

// TestGetPathsMigratesLegacyDatabaseFile is the end-to-end check for the
// already-migrated state: the new data folder exists holding a
// legacy-named database file next to an empty new-named one (as an
// earlier launch would have left it). GetPaths must repair it so the
// data is visible under the new filename.
func TestGetPathsMigratesLegacyDatabaseFile(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("legacy-path layout under test is Linux XDG paths")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, ".cache"))

	paths, err := GetPaths()
	if err != nil {
		t.Fatalf("GetPaths() returned error: %v", err)
	}
	if err := os.MkdirAll(paths.Data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.Data, legacyDatabaseFileName), []byte("dbdata"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.Data, "daak.db"), []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := GetPaths(); err != nil {
		t.Fatalf("second GetPaths() returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.Data, legacyDatabaseFileName)); !os.IsNotExist(err) {
		t.Errorf("legacy database file still exists after GetPaths")
	}
	b, err := os.ReadFile(filepath.Join(paths.Data, "daak.db"))
	if err != nil {
		t.Fatalf("new database file missing after GetPaths: %v", err)
	}
	if string(b) != "dbdata" {
		t.Errorf("new database file = %q, want %q", string(b), "dbdata")
	}
}
func TestGetPathsMigratesLegacyDataDir(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("legacy-path layout under test is Linux XDG paths")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, ".cache"))

	oldPaths, err := legacyPaths()
	if err != nil {
		t.Fatalf("legacyPaths() returned error: %v", err)
	}
	if err := os.MkdirAll(oldPaths.Data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldPaths.Data, "marker.txt"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	// The legacy database file travels with the folder and is renamed.
	if err := os.WriteFile(filepath.Join(oldPaths.Data, legacyDatabaseFileName), []byte("dbdata"), 0600); err != nil {
		t.Fatal(err)
	}

	paths, err := GetPaths()
	if err != nil {
		t.Fatalf("GetPaths() returned error: %v", err)
	}
	if _, err := os.Stat(oldPaths.Data); !os.IsNotExist(err) {
		t.Errorf("legacy data folder still exists after GetPaths")
	}
	if _, err := os.Stat(filepath.Join(paths.Data, "marker.txt")); err != nil {
		t.Errorf("marker file did not move to the new data folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.Data, legacyDatabaseFileName)); !os.IsNotExist(err) {
		t.Errorf("legacy database file still exists after GetPaths")
	}
	b, err := os.ReadFile(filepath.Join(paths.Data, "daak.db"))
	if err != nil {
		t.Fatalf("new database file missing after GetPaths: %v", err)
	}
	if string(b) != "dbdata" {
		t.Errorf("new database file = %q, want %q", string(b), "dbdata")
	}
	if got := paths.DatabasePath(); filepath.Base(got) != "daak.db" {
		t.Errorf("DatabasePath() = %q, want daak.db", got)
	}

	// Second call is a no-op: the migrated folder stays put.
	if _, err := GetPaths(); err != nil {
		t.Fatalf("second GetPaths() returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.Data, "marker.txt")); err != nil {
		t.Errorf("marker file missing after second GetPaths: %v", err)
	}
}
