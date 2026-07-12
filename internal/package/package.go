// Package shenronpackage validates and installs standalone Shenron packages.
package shenronpackage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/S1933/Shenron/internal/fsutil"
	"github.com/S1933/Shenron/internal/pivot"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"gopkg.in/yaml.v3"
)

const (
	// ManifestFileName is the required package manifest at the package root.
	ManifestFileName = "shenron-package.yaml"
	// PivotFileName is the standalone Shenron pivot at the package root.
	PivotFileName = "shenron.yaml"

	indexFileName     = "index.json"
	indexLockFileName = "index.lock"
)

var (
	kebabCase = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	semver    = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
)

// Manifest describes a standalone, shareable Shenron configuration package.
type Manifest struct {
	SchemaVersion string            `yaml:"schemaVersion"`
	Name          string            `yaml:"name"`
	Version       string            `yaml:"version"`
	Description   string            `yaml:"description"`
	Skills        SkillRequirements `yaml:"skills,omitempty"`
}

// SkillRequirements declares the skills a package expects to be present.
type SkillRequirements struct {
	Required []string `yaml:"required,omitempty"`
	Optional []string `yaml:"optional,omitempty"`
}

// Package is a validated package directory.
type Package struct {
	Root     string
	Manifest Manifest
	Pivot    *pivot.PivotFile
}

// InstalledPackage is the durable record of one installed local package.
type InstalledPackage struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Ref         string `json:"ref,omitempty"`
	Revision    string `json:"revision"`
	Root        string `json:"root"`
	Digest      string `json:"digest"`
}

// Store owns an injectable package-cache root. It is safe to use in tests
// without relying on a user's home directory.
type Store struct {
	root string
}

// NewStore creates a package store rooted at root.
func NewStore(root string) *Store {
	return &Store{root: filepath.Clean(root)}
}

// LoadDirectory parses and validates a standalone package directory.
func LoadDirectory(root string) (*Package, error) {
	realRoot, err := resolveDirectory(root)
	if err != nil {
		return nil, err
	}
	if err := validateNoSymlinks(realRoot); err != nil {
		return nil, err
	}

	manifestData, err := os.ReadFile(filepath.Join(realRoot, ManifestFileName))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ManifestFileName, err)
	}
	manifest, err := ParseManifest(manifestData)
	if err != nil {
		return nil, err
	}

	pivotData, err := os.ReadFile(filepath.Join(realRoot, PivotFileName))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", PivotFileName, err)
	}
	pf, err := pivot.ParseStrict(pivotData, realRoot)
	if err != nil {
		return nil, fmt.Errorf("validate %s: %w", PivotFileName, err)
	}
	if err := validatePromptContainment(realRoot, pf); err != nil {
		return nil, err
	}

	return &Package{Root: realRoot, Manifest: *manifest, Pivot: pf}, nil
}

func resolveDirectory(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve package root: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve package root: %w", err)
	}
	info, err := os.Stat(realRoot)
	if err != nil {
		return "", fmt.Errorf("stat package root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("package root is not a directory: %s", root)
	}
	return realRoot, nil
}

// ParseManifest decodes a strict manifest and validates its structural and
// cross-field invariants.
func ParseManifest(data []byte) (*Manifest, error) {
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ManifestFileName, err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", ManifestFileName, err)
		}
		return nil, fmt.Errorf("parse %s: multiple YAML documents are not supported", ManifestFileName)
	}
	if err := validateManifest(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func validateNoSymlinks(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root || entry.Type()&os.ModeSymlink == 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return fmt.Errorf("package contains symlink %q; symlinks are not supported", rel)
	})
}

func validateManifest(manifest *Manifest) error {
	var errs []string
	if manifest.SchemaVersion != "1" {
		errs = append(errs, `schemaVersion must be "1"`)
	}
	if !kebabCase.MatchString(manifest.Name) {
		errs = append(errs, "name must match ^[a-z][a-z0-9-]*$")
	}
	if !semver.MatchString(manifest.Version) {
		errs = append(errs, "version must be a semantic version")
	}
	if strings.TrimSpace(manifest.Description) == "" {
		errs = append(errs, "description is required")
	}
	errSkills := validateSkills(manifest.Skills)
	errs = append(errs, errSkills...)
	if len(errs) > 0 {
		return fmt.Errorf("manifest validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

func validateSkills(skills SkillRequirements) []string {
	var errs []string
	required := make(map[string]struct{}, len(skills.Required))
	for i, skill := range skills.Required {
		if !kebabCase.MatchString(skill) {
			errs = append(errs, fmt.Sprintf("skills.required[%d] must match ^[a-z][a-z0-9-]*$", i))
		}
		if _, exists := required[skill]; exists {
			errs = append(errs, fmt.Sprintf("skills.required[%d] duplicates %q", i, skill))
		}
		required[skill] = struct{}{}
	}
	seenOptional := make(map[string]struct{}, len(skills.Optional))
	for i, skill := range skills.Optional {
		if !kebabCase.MatchString(skill) {
			errs = append(errs, fmt.Sprintf("skills.optional[%d] must match ^[a-z][a-z0-9-]*$", i))
		}
		if _, exists := seenOptional[skill]; exists {
			errs = append(errs, fmt.Sprintf("skills.optional[%d] duplicates %q", i, skill))
		}
		seenOptional[skill] = struct{}{}
		if _, exists := required[skill]; exists {
			errs = append(errs, fmt.Sprintf("skills.required and skills.optional overlap on %q", skill))
		}
	}
	return errs
}

func validatePromptContainment(root string, pf *pivot.PivotFile) error {
	for i, agent := range pf.Agents {
		if strings.TrimSpace(agent.PromptFile) == "" {
			continue
		}
		candidate := filepath.Clean(filepath.Join(root, agent.PromptFile))
		if !isWithin(root, candidate) {
			return fmt.Errorf("agents[%d].promptFile escapes package root: %s", i, agent.PromptFile)
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return fmt.Errorf("agents[%d].promptFile resolve: %w", i, err)
		}
		if !isWithin(root, resolved) {
			return fmt.Errorf("agents[%d].promptFile escapes package root through symlink: %s", i, agent.PromptFile)
		}
	}
	return nil
}

func isWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// InstallLocal copies the current source into a content-addressed cache
// snapshot, then validates and identifies that immutable snapshot. A package
// name can be installed only once until update semantics are introduced by the
// CLI layer.
func (s *Store) InstallLocal(source string) (*InstalledPackage, error) {
	unlock, err := s.lockIndex()
	if err != nil {
		return nil, err
	}
	defer func() { _ = unlock() }()

	staged, err := s.stageLocalSnapshot(source)
	if err != nil {
		return nil, err
	}
	defer staged.cleanup()

	index, err := s.readIndex()
	if err != nil {
		return nil, err
	}
	for _, installed := range index.Packages {
		if installed.Name == staged.pkg.Manifest.Name {
			return nil, fmt.Errorf("package %q is already installed", staged.pkg.Manifest.Name)
		}
	}

	installed, err := s.publishStaged(staged)
	if err != nil {
		return nil, err
	}
	index.Packages = append(index.Packages, *installed)
	sort.Slice(index.Packages, func(i, j int) bool { return index.Packages[i].Name < index.Packages[j].Name })
	if err := s.writeIndex(index); err != nil {
		return nil, err
	}
	return installed, nil
}

// Install chooses a local-directory or public HTTPS Git installation based on
// source. A ref is accepted only for Git sources.
func (s *Store) Install(source, ref string) (*InstalledPackage, error) {
	if isHTTPSURL(source) {
		return s.InstallGit(source, ref)
	}
	if isNonHTTPSRemote(source) {
		return nil, fmt.Errorf("package Git source must be a public HTTPS URL")
	}
	if ref != "" {
		return nil, fmt.Errorf("--ref is only supported for public HTTPS Git sources")
	}
	return s.InstallLocal(source)
}

type stagedSnapshot struct {
	source   string
	ref      string
	revision string
	root     string
	tmp      string
	pkg      *Package
	digest   string
}

func (s *stagedSnapshot) installed(root string) InstalledPackage {
	return InstalledPackage{
		Name: s.pkg.Manifest.Name, Version: s.pkg.Manifest.Version, Description: s.pkg.Manifest.Description,
		Source: s.source, Ref: s.ref, Revision: s.revision, Root: root, Digest: s.digest,
	}
}

func (s *Store) stageLocalSnapshot(source string) (*stagedSnapshot, error) {
	sourceRoot, err := resolveDirectory(source)
	if err != nil {
		return nil, err
	}
	parent := filepath.Join(s.root, "packages")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("create package snapshot parent: %w", err)
	}
	tmp, err := os.MkdirTemp(parent, ".package-")
	if err != nil {
		return nil, fmt.Errorf("create package snapshot: %w", err)
	}
	root := filepath.Join(tmp, "contents")
	if err := copyDirectory(sourceRoot, root); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("copy package snapshot: %w", err)
	}
	pkg, err := LoadDirectory(root)
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("validate package snapshot: %w", err)
	}
	digest, err := directoryDigest(pkg.Root)
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("hash package snapshot: %w", err)
	}
	return &stagedSnapshot{source: sourceRoot, root: root, tmp: tmp, pkg: pkg, digest: digest, revision: digest}, nil
}

func (s *stagedSnapshot) cleanup() {
	_ = os.RemoveAll(s.tmp)
}

// List returns installed packages ordered by name.
func (s *Store) List() ([]InstalledPackage, error) {
	index, err := s.readIndex()
	if err != nil {
		return nil, err
	}
	return append([]InstalledPackage(nil), index.Packages...), nil
}

// Load returns an installed package together with its validated immutable
// snapshot. The recorded digest is rechecked before every use so a damaged
// cache can never be pushed.
func (s *Store) Load(name string) (*InstalledPackage, *Package, error) {
	if !kebabCase.MatchString(name) {
		return nil, nil, fmt.Errorf("package name must match ^[a-z][a-z0-9-]*$")
	}
	index, err := s.readIndex()
	if err != nil {
		return nil, nil, err
	}
	for i := range index.Packages {
		installed := index.Packages[i]
		if installed.Name != name {
			continue
		}
		if err := validateSnapshotDigest(installed.Root, installed.Digest); err != nil {
			return nil, nil, err
		}
		pkg, err := LoadDirectory(installed.Root)
		if err != nil {
			return nil, nil, fmt.Errorf("load package snapshot: %w", err)
		}
		return &installed, pkg, nil
	}
	return nil, nil, fmt.Errorf("package %q is not installed", name)
}

// StatePath returns the package-owned synchronization state path. State is
// deliberately kept outside immutable snapshots and persists across revisions
// of the same package name.
func (s *Store) StatePath(name string) string {
	return filepath.Join(s.root, "state", name, ".shenron-state.json")
}

// StateDir returns the parent directory of StatePath for a package.
func (s *Store) StateDir(name string) string {
	return filepath.Dir(s.StatePath(name))
}

// UpdateLocal validates a fresh local snapshot before replacing the active
// installed record. Existing immutable snapshots are deliberately retained.
func (s *Store) UpdateLocal(name, source string) (*InstalledPackage, error) {
	return s.update(name, func() (*stagedSnapshot, error) {
		return s.stageLocalSnapshot(source)
	})
}

// Update chooses a local-directory or public HTTPS Git update based on source.
func (s *Store) Update(name, source, ref string) (*InstalledPackage, error) {
	if isHTTPSURL(source) {
		return s.UpdateGit(name, source, ref)
	}
	if isNonHTTPSRemote(source) {
		return nil, fmt.Errorf("package Git source must be a public HTTPS URL")
	}
	if ref != "" {
		return nil, fmt.Errorf("--ref is only supported for public HTTPS Git sources")
	}
	return s.UpdateLocal(name, source)
}

// InstallGit installs a package from a public HTTPS Git repository. ref must
// name a tag or a full commit SHA; branches and HEAD are never selected.
func (s *Store) InstallGit(source, ref string) (*InstalledPackage, error) {
	if _, err := validateGitSource(source, ref); err != nil {
		return nil, err
	}
	unlock, err := s.lockIndex()
	if err != nil {
		return nil, err
	}
	defer func() { _ = unlock() }()

	staged, err := s.stageGitSnapshot(source, ref)
	if err != nil {
		return nil, err
	}
	defer staged.cleanup()
	index, err := s.readIndex()
	if err != nil {
		return nil, err
	}
	for _, installed := range index.Packages {
		if installed.Name == staged.pkg.Manifest.Name {
			return nil, fmt.Errorf("package %q is already installed", staged.pkg.Manifest.Name)
		}
	}
	installed, err := s.publishStaged(staged)
	if err != nil {
		return nil, err
	}
	index.Packages = append(index.Packages, *installed)
	sort.Slice(index.Packages, func(i, j int) bool { return index.Packages[i].Name < index.Packages[j].Name })
	if err := s.writeIndex(index); err != nil {
		return nil, err
	}
	return installed, nil
}

// UpdateGit replaces an installed package with a newly fetched, validated
// snapshot from a public HTTPS Git source.
func (s *Store) UpdateGit(name, source, ref string) (*InstalledPackage, error) {
	if _, err := validateGitSource(source, ref); err != nil {
		return nil, err
	}
	return s.update(name, func() (*stagedSnapshot, error) {
		return s.stageGitSnapshot(source, ref)
	})
}

func (s *Store) update(name string, stage func() (*stagedSnapshot, error)) (*InstalledPackage, error) {
	if !kebabCase.MatchString(name) {
		return nil, fmt.Errorf("package name must match ^[a-z][a-z0-9-]*$")
	}
	unlock, err := s.lockIndex()
	if err != nil {
		return nil, err
	}
	defer func() { _ = unlock() }()
	staged, err := stage()
	if err != nil {
		return nil, err
	}
	defer staged.cleanup()
	if staged.pkg.Manifest.Name != name {
		return nil, fmt.Errorf("updated package name %q does not match installed package %q", staged.pkg.Manifest.Name, name)
	}
	index, err := s.readIndex()
	if err != nil {
		return nil, err
	}
	position := -1
	for i, installed := range index.Packages {
		if installed.Name == name {
			position = i
			break
		}
	}
	if position == -1 {
		return nil, fmt.Errorf("package %q is not installed", name)
	}
	installed, err := s.publishStaged(staged)
	if err != nil {
		return nil, err
	}
	index.Packages[position] = *installed
	if err := s.writeIndex(index); err != nil {
		return nil, err
	}
	return installed, nil
}

func (s *Store) publishStaged(staged *stagedSnapshot) (*InstalledPackage, error) {
	target := filepath.Join(s.root, "packages", staged.pkg.Manifest.Name, staged.digest)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, fmt.Errorf("create package snapshot parent: %w", err)
	}
	if _, err := os.Stat(target); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat package snapshot: %w", err)
	} else if os.IsNotExist(err) {
		if err := os.Rename(staged.root, target); err != nil {
			return nil, fmt.Errorf("publish package snapshot: %w", err)
		}
	}
	if err := validateSnapshotDigest(target, staged.digest); err != nil {
		return nil, err
	}
	installed := staged.installed(target)
	return &installed, nil
}

func (s *Store) stageGitSnapshot(source, ref string) (*stagedSnapshot, error) {
	parent := filepath.Join(s.root, "packages")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("create package snapshot parent: %w", err)
	}
	tmp, err := os.MkdirTemp(parent, ".package-")
	if err != nil {
		return nil, fmt.Errorf("create package snapshot: %w", err)
	}
	root := filepath.Join(tmp, "contents")
	repo, err := git.PlainClone(root, false, &git.CloneOptions{URL: source, NoCheckout: true, Tags: git.AllTags})
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("clone package repository: %w", err)
	}
	revision, err := resolveGitRevision(repo, ref)
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	worktree, err := repo.Worktree()
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("open package worktree: %w", err)
	}
	if err := worktree.Checkout(&git.CheckoutOptions{Hash: revision, Force: true}); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("checkout package revision %s: %w", revision, err)
	}
	if err := os.RemoveAll(filepath.Join(root, ".git")); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("remove package Git metadata: %w", err)
	}
	pkg, err := LoadDirectory(root)
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("validate package snapshot: %w", err)
	}
	digest, err := directoryDigest(pkg.Root)
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("hash package snapshot: %w", err)
	}
	return &stagedSnapshot{source: source, ref: ref, revision: revision.String(), root: root, tmp: tmp, pkg: pkg, digest: digest}, nil
}

var commitSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func validateGitSource(source, ref string) (*url.URL, error) {
	parsed, err := url.Parse(source)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("package Git source must be a public HTTPS URL")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("package Git source must not include credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("package Git source must not include a query or fragment")
	}
	lowerPath := strings.ToLower(parsed.Path)
	for _, suffix := range []string{".zip", ".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tar.xz"} {
		if strings.HasSuffix(lowerPath, suffix) {
			return nil, fmt.Errorf("package source must be a Git repository, not an archive")
		}
	}
	if ref == "" || strings.EqualFold(ref, "head") || strings.HasPrefix(ref, "refs/heads/") {
		return nil, fmt.Errorf("package Git ref must be an immutable tag or full commit SHA")
	}
	return parsed, nil
}

func isHTTPSURL(source string) bool {
	parsed, err := url.Parse(source)
	return err == nil && parsed.Scheme == "https"
}

func isNonHTTPSRemote(source string) bool {
	if strings.HasPrefix(source, "git@") {
		return true
	}
	parsed, err := url.Parse(source)
	return err == nil && parsed.Scheme != ""
}

func resolveGitRevision(repo *git.Repository, ref string) (plumbing.Hash, error) {
	if commitSHA.MatchString(ref) {
		hash := plumbing.NewHash(ref)
		if _, err := repo.CommitObject(hash); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("resolve package commit %q: %w", ref, err)
		}
		return hash, nil
	}
	tag := strings.TrimPrefix(ref, "refs/tags/")
	if tag == "" || strings.HasPrefix(tag, "refs/") {
		return plumbing.ZeroHash, fmt.Errorf("package Git ref must be an immutable tag or full commit SHA")
	}
	if _, err := repo.Tag(tag); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("resolve package tag %q: %w", tag, err)
	}
	hash, err := repo.ResolveRevision(plumbing.Revision("refs/tags/" + tag))
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("resolve package tag %q: %w", tag, err)
	}
	if _, err := repo.CommitObject(*hash); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("resolve package tag %q to commit: %w", tag, err)
	}
	return *hash, nil
}

type packageIndex struct {
	Version  string             `json:"version"`
	Packages []InstalledPackage `json:"packages"`
}

func (s *Store) indexPath() string { return filepath.Join(s.root, indexFileName) }

func (s *Store) lockIndex() (func() error, error) {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return nil, fmt.Errorf("create package cache: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(s.root, indexLockFileName), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open package index lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock package index: %w", err)
	}
	return func() error {
		unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		closeErr := file.Close()
		if unlockErr != nil {
			return fmt.Errorf("unlock package index: %w", unlockErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close package index lock: %w", closeErr)
		}
		return nil
	}, nil
}

func (s *Store) readIndex() (*packageIndex, error) {
	data, err := os.ReadFile(s.indexPath())
	if os.IsNotExist(err) {
		return &packageIndex{Version: "1"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read package index: %w", err)
	}
	var index packageIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("parse package index: %w", err)
	}
	if index.Version != "1" {
		return nil, fmt.Errorf("unsupported package index version %q", index.Version)
	}
	sort.Slice(index.Packages, func(i, j int) bool { return index.Packages[i].Name < index.Packages[j].Name })
	return &index, nil
}

func (s *Store) writeIndex(index *packageIndex) error {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("encode package index: %w", err)
	}
	data = append(data, '\n')
	if err := fsutil.WriteFileAtomic(s.indexPath(), data, 0o644); err != nil {
		return fmt.Errorf("write package index: %w", err)
	}
	return nil
}

func directoryDigest(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(hash, rel+"\x00"); err != nil {
			return err
		}
		if entry.IsDir() {
			_, err := io.WriteString(hash, "dir\x00")
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_, err = io.WriteString(hash, "link\x00"+link+"\x00")
			return err
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported file type: %s", rel)
		}
		if _, err := io.WriteString(hash, "file\x00"); err != nil {
			return err
		}
		file, err := openRegularFileNoFollow(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validateSnapshotDigest(root, expectedDigest string) error {
	pkg, err := LoadDirectory(root)
	if err != nil {
		return fmt.Errorf("validate package snapshot: %w", err)
	}
	digest, err := directoryDigest(pkg.Root)
	if err != nil {
		return fmt.Errorf("hash package snapshot: %w", err)
	}
	if digest != expectedDigest {
		return fmt.Errorf("package snapshot digest mismatch: got %s, want %s", digest, expectedDigest)
	}
	return nil
}

func copyDirectory(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := destination
		if rel != "." {
			target = filepath.Join(destination, rel)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported in package snapshots: %s", rel)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported file type: %s", rel)
		}
		input, err := openRegularFileNoFollow(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeOutErr := output.Close()
		closeInErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOutErr != nil {
			return closeOutErr
		}
		return closeInErr
	})
}

func openRegularFileNoFollow(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFREG {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("not a regular file")
	}
	return os.NewFile(uintptr(fd), path), nil
}
