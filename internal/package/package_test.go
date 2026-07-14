package shenronpackage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
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

func TestLoadDirectoryRejectsUnknownPivotFields(t *testing.T) {
	tests := []struct {
		name  string
		pivot string
	}{
		{
			name: "root",
			pivot: `version: "1"
unexpected: value
agents: []`,
		},
		{
			name: "agent",
			pivot: `version: "1"
agents:
  - id: review
    description: Review code.
    mode: subagent
    unexpected: value`,
		},
		{
			name: "command",
			pivot: `version: "1"
commands:
  - id: review
    description: Review code.
    template: Review $ARGUMENTS
    unexpected: value`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadDirectory(writePackage(t, validManifest(), tt.pivot))
			if err == nil || !strings.Contains(err.Error(), "field unexpected not found") {
				t.Fatalf("LoadDirectory() error = %v, want unknown-field error", err)
			}
		})
	}
}

func TestLoadDirectoryAllowsArbitraryExtensions(t *testing.T) {
	root := writePackage(t, validManifest(), `version: "1"
agents:
  - id: review
    description: Review code.
    mode: subagent
    extensions:
      thirdParty:
        enabled: true
`)

	pkg, err := LoadDirectory(root)
	if err != nil {
		t.Fatalf("LoadDirectory() error = %v", err)
	}
	thirdParty, ok := pkg.Pivot.Agents[0].Extensions["thirdParty"].(map[string]any)
	if !ok || thirdParty["enabled"] != true {
		t.Fatalf("extensions = %#v, want thirdParty.enabled preserved", pkg.Pivot.Agents[0].Extensions)
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
	if err == nil || !strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("LoadDirectory() error = %v, want symlink rejection", err)
	}
}

func TestLoadDirectoryRejectsContainedSymlinks(t *testing.T) {
	tests := []struct {
		name           string
		linkPath       string
		target         string
		absoluteTarget bool
	}{
		{name: "manifest", linkPath: ManifestFileName, target: "manifest-source.yaml"},
		{name: "pivot", linkPath: PivotFileName, target: "pivot-source.yaml", absoluteTarget: true},
		{name: "prompt", linkPath: "prompts/review.md", target: "review-source.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writePackage(t, validManifest(), validPivot())
			link := filepath.Join(root, tt.linkPath)
			target := filepath.Join(filepath.Dir(link), tt.target)
			if tt.linkPath == ManifestFileName || tt.linkPath == PivotFileName {
				target = filepath.Join(root, tt.target)
			}
			contents, err := os.ReadFile(link)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, target, string(contents))
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			linkTarget := tt.target
			if tt.absoluteTarget {
				linkTarget = target
			}
			if err := os.Symlink(linkTarget, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}

			_, err = LoadDirectory(root)
			if err == nil || !strings.Contains(err.Error(), "symlinks are not supported") {
				t.Fatalf("LoadDirectory() error = %v, want symlink rejection", err)
			}
		})
	}
}

func TestStoreInstallLocalRejectsContainedSymlink(t *testing.T) {
	source := writePackage(t, validManifest(), validPivot())
	prompt := filepath.Join(source, "prompts", "review.md")
	target := filepath.Join(source, "prompts", "review-source.md")
	contents, err := os.ReadFile(prompt)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, target, string(contents))
	if err := os.Remove(prompt); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("review-source.md", prompt); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err = NewStore(filepath.Join(t.TempDir(), "cache")).InstallLocal(source)
	if err == nil || !strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("InstallLocal() error = %v, want symlink rejection", err)
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

func TestStoreInstallLocalUsesSnapshotIdentityWhenSourceChangesWhileWaitingForIndexLock(t *testing.T) {
	source := writePackage(t, validManifest(), validPivot())
	store := NewStore(filepath.Join(t.TempDir(), "cache"))

	unlock, err := store.lockIndex()
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := store.InstallLocal(source)
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("InstallLocal() completed while index lock held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	writeFile(t, filepath.Join(source, ManifestFileName), `schemaVersion: "1"
name: changed-package
version: 9.9.9
description: Changed after staging.
`)
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatalf("InstallLocal() error = %v", err)
	}
	installed, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 1 || installed[0].Name != "changed-package" || installed[0].Version != "9.9.9" {
		t.Fatalf("List() = %+v, want changed snapshot package", installed)
	}
}

func TestOpenRegularFileNoFollowRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	writeFile(t, target, "outside")
	link := filepath.Join(root, "source")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	file, err := openRegularFileNoFollow(link)
	if file != nil {
		_ = file.Close()
	}
	if err == nil {
		t.Fatal("openRegularFileNoFollow() error = nil, want symlink rejection")
	}
}

func TestStoreInstallLocalRejectsExistingSnapshotWithDifferentDigest(t *testing.T) {
	source := writePackage(t, validManifest(), validPivot())
	store := NewStore(filepath.Join(t.TempDir(), "cache"))
	pkg, err := LoadDirectory(source)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := directoryDigest(pkg.Root)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(store.root, "packages", pkg.Manifest.Name, digest)
	if err := copyDirectory(pkg.Root, target); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(target, "prompts", "review.md"), "Corrupted cached content.")

	_, err = store.InstallLocal(source)
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("InstallLocal() error = %v, want snapshot digest mismatch", err)
	}
	installed, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 0 {
		t.Fatalf("List() = %+v, want no indexed packages", installed)
	}
}

func TestStoreInstallLocalValidatesExistingSnapshotBeforeIndexing(t *testing.T) {
	source := writePackage(t, validManifest(), validPivot())
	store := NewStore(filepath.Join(t.TempDir(), "cache"))
	pkg, err := LoadDirectory(source)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := directoryDigest(pkg.Root)
	if err != nil {
		t.Fatal(err)
	}
	stagedRoot := filepath.Join(store.root, "packages", pkg.Manifest.Name, digest)
	writeFile(t, filepath.Join(stagedRoot, ManifestFileName), validManifest())
	writeFile(t, filepath.Join(stagedRoot, PivotFileName), validPivot())
	prompt := filepath.Join(stagedRoot, "prompts", "review.md")
	writeFile(t, prompt, "Review this change.")
	if err := os.Remove(prompt); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("review-source.md", prompt); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeFile(t, filepath.Join(stagedRoot, "prompts", "review-source.md"), "Review this change.")

	_, err = store.InstallLocal(source)
	if err == nil || !strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("InstallLocal() error = %v, want staged snapshot validation failure", err)
	}
	installed, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 0 {
		t.Fatalf("List() = %+v, want no indexed packages", installed)
	}
}

func TestStoreInstallLocalWaitsForIndexLock(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "cache"))
	unlock, err := store.lockIndex()
	if err != nil {
		t.Fatal(err)
	}
	// Capture unlock by reference so we can neutralize it after explicit release.
	released := false
	release := func() error {
		if released {
			return nil
		}
		released = true
		return unlock()
	}
	defer func() { _ = release() }()

	result := make(chan error, 1)
	go func() {
		_, err := store.InstallLocal(writePackage(t, validManifest(), validPivot()))
		result <- err
	}()

	select {
	case err := <-result:
		t.Fatalf("InstallLocal() completed while index lock held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatalf("InstallLocal() after unlocking = %v", err)
	}
}

func TestParseManifestValidatesPrereleaseNumericIdentifiers(t *testing.T) {
	tests := []struct {
		version string
		valid   bool
	}{
		{version: "1.2.3-01", valid: false},
		{version: "1.2.3-01a", valid: true},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			_, err := ParseManifest([]byte(strings.Replace(validManifest(), "1.2.3", tt.version, 1)))
			if tt.valid && err != nil {
				t.Fatalf("ParseManifest() error = %v, want valid semantic version", err)
			}
			if !tt.valid && (err == nil || !strings.Contains(err.Error(), "version must be a semantic version")) {
				t.Fatalf("ParseManifest() error = %v, want semantic version error", err)
			}
		})
	}
}

func TestParseManifestRejectsSecondDocument(t *testing.T) {
	_, err := ParseManifest([]byte(validManifest() + "---\nname: ignored\n"))
	if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("ParseManifest() error = %v, want multiple-document error", err)
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

func TestStoreUpdateLocalReplacesActiveSnapshot(t *testing.T) {
	source := writePackage(t, validManifest(), validPivot())
	store := NewStore(filepath.Join(t.TempDir(), "cache"))
	installed, err := store.InstallLocal(source)
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(source, ManifestFileName), strings.Replace(validManifest(), "1.2.3", "1.2.4", 1))
	writeFile(t, filepath.Join(source, PivotFileName), `version: "1"
agents:
  - id: updated
    description: Updated agent.
    mode: subagent
`)
	updated, err := store.UpdateLocal("acme-reviewers", source)
	if err != nil {
		t.Fatalf("UpdateLocal() error = %v", err)
	}
	if updated.Version != "1.2.4" || updated.Digest == installed.Digest || updated.Root == installed.Root {
		t.Fatalf("updated = %+v, original = %+v", updated, installed)
	}
	packages, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages[0] != *updated {
		t.Fatalf("List() = %+v, want active update %+v", packages, updated)
	}
}

func TestStoreUpdateLocalKeepsActiveSnapshotWhenReplacementIsInvalid(t *testing.T) {
	source := writePackage(t, validManifest(), validPivot())
	store := NewStore(filepath.Join(t.TempDir(), "cache"))
	installed, err := store.InstallLocal(source)
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(source, ManifestFileName), `schemaVersion: "1"
name: a-different-package
version: 1.2.4
description: A different package.
`)
	if _, err := store.UpdateLocal("acme-reviewers", source); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("UpdateLocal() error = %v, want identity error", err)
	}
	packages, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages[0] != *installed {
		t.Fatalf("List() = %+v, want original active package %+v", packages, installed)
	}
}

func TestStoreInstallGitRejectsUnsafeSourcesAndMutableRefs(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "cache"))
	tests := []struct {
		name, source, ref, want string
	}{
		{"credentials", "https://token@example.com/acme/reviewers.git", "v1.2.3", "credentials"},
		{"ssh-password", "ssh://git:secret@github.com/acme/reviewers.git", "v1.2.3", "credentials"},
		{"archive", "https://example.com/reviewers.tar.gz", "v1.2.3", "Git repository"},
		{"ssh-archive", "git@github.com:acme/reviewers.tar.gz", "v1.2.3", "Git repository"},
		{"branch", "https://example.com/acme/reviewers.git", "refs/heads/main", "immutable"},
		{"head", "https://example.com/acme/reviewers.git", "HEAD", "immutable"},
		{"ssh-head", "git@github.com:acme/reviewers.git", "HEAD", "immutable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.InstallGit(tt.source, tt.ref)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("InstallGit() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestStoreInstallRejectsNonHTTPSRemoteSources(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "cache"))
	for _, source := range []string{"http://example.com/acme/reviewers.git", "git://example.com/acme/reviewers.git", "ftp://example.com/acme/reviewers.git"} {
		if _, err := store.Install(source, ""); err == nil || !strings.Contains(err.Error(), "public HTTPS or SSH") {
			t.Errorf("Install(%q) error = %v, want HTTPS/SSH source rejection", source, err)
		}
	}
}

func TestGitSourceClassification(t *testing.T) {
	tests := []struct {
		source               string
		https, ssh, nonHTTPS bool
	}{
		{"https://github.com/acme/pkg.git", true, false, false},
		{"git@github.com:acme/pkg.git", false, true, true},
		{"ssh://git@github.com/acme/pkg.git", false, true, true},
		{"http://example.com/acme/pkg.git", false, false, true},
		{"git://example.com/acme/pkg.git", false, false, true},
		{"testdata/local-package", false, false, false},
		{"/abs/path/to/package", false, false, false},
		{"./relative/package", false, false, false},
	}
	for _, tt := range tests {
		if got := isHTTPSURL(tt.source); got != tt.https {
			t.Errorf("isHTTPSURL(%q) = %v, want %v", tt.source, got, tt.https)
		}
		if got := isSSHURL(tt.source); got != tt.ssh {
			t.Errorf("isSSHURL(%q) = %v, want %v", tt.source, got, tt.ssh)
		}
		if got := isRemoteGitSource(tt.source); got != (tt.https || tt.ssh) {
			t.Errorf("isRemoteGitSource(%q) = %v, want %v", tt.source, got, tt.https || tt.ssh)
		}
	}
}

func TestValidateGitSourceAcceptsSSH(t *testing.T) {
	sources := []string{
		"git@github.com:acme/reviewers.git",
		"ssh://git@github.com/acme/reviewers.git",
		"ssh://git@github.com:2222/acme/reviewers.git",
	}
	for _, source := range sources {
		if err := validateGitSource(source, "v1.2.3"); err != nil {
			t.Errorf("validateGitSource(%q, tag) = %v, want nil", source, err)
		}
		fullSHA := "0123456789abcdef0123456789abcdef01234567"
		if err := validateGitSource(source, fullSHA); err != nil {
			t.Errorf("validateGitSource(%q, sha) = %v, want nil", source, err)
		}
	}
}

func TestResolveGitRevisionAcceptsTagAndFullCommitSHA(t *testing.T) {
	root := t.TempDir()
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "README.md"), "package")
	if _, err := worktree.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	commit, err := worktree.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateTag("v1.2.3", commit, nil); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"v1.2.3", commit.String()} {
		resolved, err := resolveGitRevision(repo, ref)
		if err != nil {
			t.Fatalf("resolveGitRevision(%q) error = %v", ref, err)
		}
		if resolved != commit {
			t.Errorf("resolveGitRevision(%q) = %s, want %s", ref, resolved, commit)
		}
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
