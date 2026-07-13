package fsutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const journalFileName = ".shenron-journal.json"
const journalVersion = "1"

type journalEntry struct {
	Final string `json:"final"`
	Temp  string `json:"temp"`
}

type journalDoc struct {
	Version string         `json:"version"`
	Entries []journalEntry `json:"entries"`
}

// Transaction stages file writes as temp files and commits them as a batch: it
// records a journal listing every pending rename, renames each staged temp file
// into place, then removes the journal. A crash after the journal is written is
// repaired by RecoverTransaction, which replays the pending renames
// (roll-forward). Individual renames are atomic; the journal makes the batch
// recoverable so a mid-push crash never leaves a half-applied set with no way to
// finish it.
//
// A Transaction is not safe for concurrent use.
type Transaction struct {
	dir     string
	entries []journalEntry
}

// NewTransaction creates a transaction whose journal lives in dir (typically the
// state directory beside .shenron-state.json).
func NewTransaction(dir string) *Transaction {
	return &Transaction{dir: dir}
}

// Stage writes data to a temp file in the destination directory and records the
// pending rename. The file is not visible at its final path until Commit.
func (t *Transaction) Stage(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent directories: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".shenron-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	t.entries = append(t.entries, journalEntry{Final: path, Temp: tmpPath})
	return nil
}

// Commit journals the pending renames, performs them, then removes the journal.
// With nothing staged it is a no-op. If a rename fails partway, the journal is
// left in place so RecoverTransaction can finish the batch on the next run.
func (t *Transaction) Commit() error {
	if len(t.entries) == 0 {
		return nil
	}
	if err := t.writeJournal(); err != nil {
		t.Discard()
		return err
	}
	for _, e := range t.entries {
		if err := os.Rename(e.Temp, e.Final); err != nil {
			return fmt.Errorf("rename %s: %w", e.Final, err)
		}
	}
	return t.removeJournal()
}

// Discard removes any staged temp files without touching their destinations.
// Safe to call after a successful Commit (the temp files are already gone).
func (t *Transaction) Discard() {
	for _, e := range t.entries {
		_ = os.Remove(e.Temp)
	}
	t.entries = nil
}

func (t *Transaction) journalPath() string { return filepath.Join(t.dir, journalFileName) }

func (t *Transaction) writeJournal() error {
	data, err := json.MarshalIndent(journalDoc{Version: journalVersion, Entries: t.entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal journal: %w", err)
	}
	if err := WriteFileAtomic(t.journalPath(), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write journal: %w", err)
	}
	return nil
}

func (t *Transaction) removeJournal() error {
	if err := os.Remove(t.journalPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove journal: %w", err)
	}
	return nil
}

// RecoverTransaction completes a push interrupted after its journal was written:
// it replays the pending renames (roll-forward) and removes the journal. Entries
// whose temp file is already gone (rename completed before the crash) are
// skipped. It is a no-op when no journal is present.
func RecoverTransaction(dir string) error {
	path := filepath.Join(dir, journalFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read journal: %w", err)
	}

	var doc journalDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse journal: %w", err)
	}

	for _, e := range doc.Entries {
		if _, err := os.Stat(e.Temp); err != nil {
			continue // already renamed (or gone) — nothing to complete
		}
		if err := os.Rename(e.Temp, e.Final); err != nil {
			return fmt.Errorf("recover rename %s: %w", e.Final, err)
		}
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove journal after recovery: %w", err)
	}
	return nil
}
