package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/S1933/Shenron/internal/cli"
	shenronpackage "github.com/S1933/Shenron/internal/package"
)

func TestRunPackageInstallListAndUpdateLocal(t *testing.T) {
	source := writeCLIPackage(t, "1.2.3")
	store := shenronpackage.NewStore(filepath.Join(t.TempDir(), "cache"))
	var output bytes.Buffer

	if err := cli.RunPackageInstall(cli.PackageInstallOptions{Store: store, Source: source, Output: &output}); err != nil {
		t.Fatalf("RunPackageInstall() error = %v", err)
	}
	if !strings.Contains(output.String(), "installed package acme-reviewers@1.2.3") {
		t.Fatalf("install output = %q", output.String())
	}

	output.Reset()
	if err := cli.RunPackageList(cli.PackageListOptions{Store: store, Output: &output}); err != nil {
		t.Fatalf("RunPackageList() error = %v", err)
	}
	if !strings.Contains(output.String(), "acme-reviewers\t1.2.3") {
		t.Fatalf("list output = %q", output.String())
	}

	writeCLIFile(t, filepath.Join(source, shenronpackage.ManifestFileName), `schemaVersion: "1"
name: acme-reviewers
version: 1.2.4
description: Shared reviewers.
`)
	output.Reset()
	if err := cli.RunPackageUpdate(cli.PackageUpdateOptions{Store: store, Name: "acme-reviewers", Source: source, Output: &output}); err != nil {
		t.Fatalf("RunPackageUpdate() error = %v", err)
	}
	if !strings.Contains(output.String(), "updated package acme-reviewers@1.2.4") {
		t.Fatalf("update output = %q", output.String())
	}
}

func writeCLIPackage(t *testing.T, version string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "package")
	writeCLIFile(t, filepath.Join(root, shenronpackage.ManifestFileName), `schemaVersion: "1"
name: acme-reviewers
version: `+version+`
description: Shared reviewers.
`)
	writeCLIFile(t, filepath.Join(root, shenronpackage.PivotFileName), `version: "1"
agents: []
`)
	return root
}

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
