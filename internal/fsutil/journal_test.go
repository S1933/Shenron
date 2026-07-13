package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTransactionCommitWritesFilesAndRemovesJournal(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a", "one.txt")
	pathB := filepath.Join(dir, "b", "two.txt")

	tx := NewTransaction(dir)
	if err := tx.Stage(pathA, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage(pathB, []byte("beta"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Nothing is visible before commit.
	if _, err := os.Stat(pathA); !os.IsNotExist(err) {
		t.Fatalf("file A visible before commit: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	assertFile(t, pathA, "alpha", 0o644)
	assertFile(t, pathB, "beta", 0o600)

	if _, err := os.Stat(filepath.Join(dir, journalFileName)); !os.IsNotExist(err) {
		t.Errorf("journal not removed after commit: %v", err)
	}
}

// TestRecoverCompletesInterruptedRenames simulates a crash after the journal was
// written but before any rename ran: the staged temp files and journal are on
// disk, the finals are not. RecoverTransaction must roll the batch forward.
func TestRecoverCompletesInterruptedRenames(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "one.txt")
	pathB := filepath.Join(dir, "sub", "two.txt")

	tx := NewTransaction(dir)
	if err := tx.Stage(pathA, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage(pathB, []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Write the journal but stop before renaming — the crash point.
	if err := tx.writeJournal(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pathA); !os.IsNotExist(err) {
		t.Fatalf("final should not exist before recovery: %v", err)
	}

	// A fresh process would only see the journal and temp files.
	if err := RecoverTransaction(dir); err != nil {
		t.Fatal(err)
	}

	assertFile(t, pathA, "alpha", 0o644)
	assertFile(t, pathB, "beta", 0o644)
	if _, err := os.Stat(filepath.Join(dir, journalFileName)); !os.IsNotExist(err) {
		t.Errorf("journal not removed after recovery: %v", err)
	}
}

func TestRecoverPartialRenamesIsResumable(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "one.txt")
	pathB := filepath.Join(dir, "two.txt")

	tx := NewTransaction(dir)
	if err := tx.Stage(pathA, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage(pathB, []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tx.writeJournal(); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after the first rename completed but before the second.
	if err := os.Rename(tx.entries[0].Temp, tx.entries[0].Final); err != nil {
		t.Fatal(err)
	}

	if err := RecoverTransaction(dir); err != nil {
		t.Fatal(err)
	}
	assertFile(t, pathA, "alpha", 0o644)
	assertFile(t, pathB, "beta", 0o644)
}

func TestRecoverNoJournalIsNoop(t *testing.T) {
	if err := RecoverTransaction(t.TempDir()); err != nil {
		t.Fatalf("recover with no journal should be a no-op, got %v", err)
	}
}

func assertFile(t *testing.T, path, want string, perm os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Errorf("%s = %q, want %q", path, data, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != perm {
		t.Errorf("%s mode = %o, want %o", path, info.Mode().Perm(), perm)
	}
}
