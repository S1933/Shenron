package shenronpackage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDirectoryAcceptsValidPackage(t *testing.T) {
	root := writePackage(t, `schemaVersion: "1"
name: acme-reviewers
version: 1.2.3
description: Shared reviewers.
skills:
  required: [verification-before-completion]
  optional: [requesting-code-review]
`, `version: "1"
agents:
  - id: review
    description: Review code.
    mode: subagent
    promptFile: prompts/review.md
`)

	pkg, err := LoadDirectory(root)
	if err != nil {
		t.Fatalf("LoadDirectory() error = %v", err)
	}
	if pkg.Manifest.Name != "acme-reviewers" {
		t.Errorf("name = %q, want acme-reviewers", pkg.Manifest.Name)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Root != resolvedRoot {
		t.Errorf("root = %q, want %q", pkg.Root, resolvedRoot)
	}
}

func TestLoadDirectoryRejectsInvalidManifest(t *testing.T) {
	root := writePackage(t, `schemaVersion: "2"
name: Acme Reviewers
version: not-a-version
description: ""
skills:
  required: [valid-skill, duplicate]
  optional: [duplicate, Invalid Skill]
`, validPivot())

	_, err := LoadDirectory(root)
	if err == nil {
		t.Fatal("LoadDirectory() error = nil, want validation failure")
	}
	for _, want := range []string{
		"schemaVersion must be \"1\"", "name must match", "version must be a semantic version",
		"description is required", "skills.optional[1] must match", "skills.required and skills.optional overlap",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want %q", err, want)
		}
	}
}

func TestLoadDirectoryRejectsUnknownManifestField(t *testing.T) {
	root := writePackage(t, `schemaVersion: "1"
name: acme-reviewers
version: 1.2.3
description: Shared reviewers.
unexpected: value
`, validPivot())

	_, err := LoadDirectory(root)
	if err == nil || !strings.Contains(err.Error(), "field unexpected not found") {
		t.Fatalf("LoadDirectory() error = %v, want unknown-field error", err)
	}
}

func TestLoadDirectoryRequiresRootPivot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ManifestFileName), validManifest())

	_, err := LoadDirectory(root)
	if err == nil || !strings.Contains(err.Error(), PivotFileName) {
		t.Fatalf("LoadDirectory() error = %v, want missing pivot error", err)
	}
}

func TestLoadDirectoryRejectsPromptFileOutsidePackageRoot(t *testing.T) {
	root := writePackage(t, validManifest(), `version: "1"
agents:
  - id: review
    description: Review code.
    mode: subagent
    promptFile: ../outside.md
`)
	writeFile(t, filepath.Join(filepath.Dir(root), "outside.md"), "outside")

	_, err := LoadDirectory(root)
	if err == nil || !strings.Contains(err.Error(), "escapes package root") {
		t.Fatalf("LoadDirectory() error = %v, want containment error", err)
	}
}

func TestLoadDirectoryRejectsPromptFileSymlinkEscape(t *testing.T) {
	root := writePackage(t, validManifest(), `version: "1"
agents:
  - id: review
    description: Review code.
    mode: subagent
    promptFile: prompts/review.md
`)
	outside := filepath.Join(t.TempDir(), "outside.md")
	writeFile(t, outside, "outside")
	promptPath := filepath.Join(root, "prompts", "review.md")
	if err := os.Remove(promptPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, promptPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := LoadDirectory(root)
	if err == nil || !strings.Contains(err.Error(), "escapes package root") {
		t.Fatalf("LoadDirectory() error = %v, want symlink containment error", err)
	}
}

func TestStoreInstallLocalCreatesIndependentContentAddressedSnapshot(t *testing.T) {
	source := writePackage(t, validManifest(), validPivot())
	store := NewStore(filepath.Join(t.TempDir(), "cache"))

	installed, err := store.InstallLocal(source)
	if err != nil {
		t.Fatalf("InstallLocal() error = %v", err)
	}
	if installed.Digest == "" || !strings.Contains(installed.Root, installed.Digest) {
		t.Errorf("installed = %+v, want content-addressed root", installed)
	}
	if got, err := os.ReadFile(filepath.Join(installed.Root, PivotFileName)); err != nil || string(got) != validPivot() {
		t.Fatalf("snapshot pivot = %q, %v", got, err)
	}

	writeFile(t, filepath.Join(source, PivotFileName), `version: "1"
agents: []
`)
	got, err := os.ReadFile(filepath.Join(installed.Root, PivotFileName))
	if err != nil || string(got) != validPivot() {
		t.Fatalf("snapshot changed with source: %q, %v", got, err)
	}
}

func TestStoreInstallLocalRefusesDuplicatePackageName(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "cache"))
	if _, err := store.InstallLocal(writePackage(t, validManifest(), validPivot())); err != nil {
		t.Fatalf("first InstallLocal() error = %v", err)
	}

	other := writePackage(t, validManifest(), `version: "1"
agents:
  - id: another
    description: Another agent.
    mode: primary
`)
	_, err := store.InstallLocal(other)
	if err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("second InstallLocal() error = %v, want duplicate-name error", err)
	}
}

func TestStoreListReturnsInstalledLocalPackages(t *testing.T) {
	source := writePackage(t, validManifest(), validPivot())
	store := NewStore(filepath.Join(t.TempDir(), "cache"))
	installed, err := store.InstallLocal(source)
	if err != nil {
		t.Fatal(err)
	}

	packages, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages[0].Name != "acme-reviewers" || packages[0].Digest != installed.Digest || packages[0].Source != resolvedSource {
		t.Fatalf("List() = %+v, want installed package", packages)
	}
}

func writePackage(t *testing.T, manifest, pivot string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "package")
	writeFile(t, filepath.Join(root, ManifestFileName), manifest)
	writeFile(t, filepath.Join(root, PivotFileName), pivot)
	writeFile(t, filepath.Join(root, "prompts", "review.md"), "Review this change.")
	return root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func validManifest() string {
	return `schemaVersion: "1"
name: acme-reviewers
version: 1.2.3
description: Shared reviewers.
`
}

func validPivot() string {
	return `version: "1"
agents:
  - id: review
    description: Review code.
    mode: subagent
    promptFile: prompts/review.md
`
}
