package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/S1933/Shenron/internal/cli"
	shenronpackage "github.com/S1933/Shenron/internal/package"
)

func TestDoctorEmptyStore(t *testing.T) {
	store := shenronpackage.NewStore(filepath.Join(t.TempDir(), "cache"))
	var buf bytes.Buffer
	if err := cli.RunDoctor(cli.DoctorOptions{Store: store, Output: &buf}); err != nil {
		t.Fatalf("doctor on empty store should pass: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "no packages installed") {
		t.Errorf("expected no-packages note, got:\n%s", out)
	}
	for _, target := range []string{"claude-code", "codex", "opencode"} {
		if !strings.Contains(out, "target "+target) {
			t.Errorf("expected target %q check, got:\n%s", target, out)
		}
	}
}

func TestDoctorInstalledPackageJSON(t *testing.T) {
	source := writeCLIPackage(t, "1.2.3")
	store := shenronpackage.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err := cli.RunPackageInstall(cli.PackageInstallOptions{Store: store, Source: source, Output: io.Discard}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := cli.RunDoctor(cli.DoctorOptions{Store: store, Format: "json", Output: &buf}); err != nil {
		t.Fatalf("doctor: %v", err)
	}

	var report cli.DoctorReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("doctor output is not valid JSON: %v\n%s", err, buf.String())
	}
	if !report.OK {
		t.Fatalf("expected healthy report, got %+v", report)
	}
	found := false
	for _, c := range report.Checks {
		if strings.HasPrefix(c.Name, "package acme-reviewers@1.2.3") {
			found = true
			if c.Status != "ok" {
				t.Errorf("package check status = %q, want ok: %+v", c.Status, c)
			}
		}
	}
	if !found {
		t.Errorf("expected a check for the installed package, got %+v", report.Checks)
	}
}

func TestDoctorDetectsCorruptSnapshot(t *testing.T) {
	source := writeCLIPackage(t, "1.2.3")
	store := shenronpackage.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err := cli.RunPackageInstall(cli.PackageInstallOptions{Store: store, Source: source, Output: io.Discard}); err != nil {
		t.Fatal(err)
	}

	installed, err := store.List()
	if err != nil || len(installed) != 1 {
		t.Fatalf("list: %v (%d packages)", err, len(installed))
	}
	// Corrupt the immutable snapshot so its digest no longer matches.
	pivotPath := filepath.Join(installed[0].Root, shenronpackage.PivotFileName)
	if err := os.WriteFile(pivotPath, []byte("version: \"1\"\nagents: []\n# tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err = cli.RunDoctor(cli.DoctorOptions{Store: store, Output: &buf})
	if !errors.Is(err, cli.ErrDoctorFailed) {
		t.Fatalf("expected ErrDoctorFailed on corrupt snapshot, got: %v", err)
	}
	if !strings.Contains(buf.String(), "snapshot invalid") {
		t.Errorf("expected snapshot-invalid detail, got:\n%s", buf.String())
	}
}
