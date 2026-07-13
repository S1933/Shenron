package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/S1933/Shenron/internal/adapter"
	"github.com/S1933/Shenron/internal/diff"
	"github.com/S1933/Shenron/internal/fsutil"
	shenronpackage "github.com/S1933/Shenron/internal/package"
	"github.com/S1933/Shenron/internal/pivot"
	"github.com/spf13/cobra"
)

var (
	// ErrPackagePermissions indicates that a package needs explicit permission approval.
	ErrPackagePermissions = errors.New("package permissions require approval")
	// ErrPackageSkills indicates that a package's required skills are unavailable.
	ErrPackageSkills = errors.New("package required skills are unavailable")
	// ErrPackageCollision indicates that a package would take over a foreign native resource.
	ErrPackageCollision = errors.New("package output collides with foreign native configuration")
)

// PackageDiffOptions configures package diff for tests and embedding.
type PackageDiffOptions struct {
	Store     *shenronpackage.Store
	Name      string
	Target    string
	Adapters  map[string]adapter.Adapter
	SkillsDir string
	Format    string // "text" (default) or "json"
	Output    io.Writer
}

// PackagePushOptions configures an explicit standalone package push.
type PackagePushOptions struct {
	Store            *shenronpackage.Store
	Name             string
	Target           string
	Force            bool
	AllowPermissions bool
	Adapters         map[string]adapter.Adapter
	SkillsDir        string
	Format           string // "text" (default) or "json"
	Output           io.Writer
}

type packageApproval struct {
	Revision         string `json:"revision"`
	PermissionDigest string `json:"permissionDigest"`
}

// RunPackageDiff shows a package's target diff and reports every permission
// grant and missing package skill without modifying native configuration.
func RunPackageDiff(opts PackageDiffOptions) error {
	format, err := parseOutputFormat(opts.Format)
	if err != nil {
		return err
	}
	installed, pkg, err := packageStore(opts.Store).Load(opts.Name)
	if err != nil {
		return err
	}
	output := packageOutput(opts.Output)
	// Keep stdout pure JSON: send the human requirements preamble to stderr.
	preamble := output
	if format == formatJSON {
		preamble = os.Stderr
	}
	required, optional := missingPackageSkills(pkg.Manifest.Skills, opts.SkillsDir)
	if err := printPackageRequirements(preamble, permissionGrants(pkg.Pivot), required, optional); err != nil {
		return err
	}
	return runDiffAt(filepath.Join(installed.Root, shenronpackage.PivotFileName), opts.Target, opts.Adapters, packageStore(opts.Store).StateDir(installed.Name), format, output, os.Stderr)
}

// RunPackagePush applies exactly one installed package. Package state and
// permission approvals are deliberately stored alongside the package cache,
// never inside its immutable pivot snapshot.
func RunPackagePush(opts PackagePushOptions) error {
	format, err := parseOutputFormat(opts.Format)
	if err != nil {
		return err
	}
	store := packageStore(opts.Store)
	installed, pkg, err := store.Load(opts.Name)
	if err != nil {
		return err
	}
	output := packageOutput(opts.Output)
	// Keep stdout pure JSON: warnings go to stderr in JSON mode.
	warnOut := output
	if format == formatJSON {
		warnOut = os.Stderr
	}
	required, optional := missingPackageSkills(pkg.Manifest.Skills, opts.SkillsDir)
	if len(optional) > 0 {
		if _, err := fmt.Fprintf(warnOut, "warning: optional package skills unavailable: %s\n", strings.Join(optional, ", ")); err != nil {
			return err
		}
	}
	if len(required) > 0 {
		return fmt.Errorf("%w: %s", ErrPackageSkills, strings.Join(required, ", "))
	}

	grants := permissionGrants(pkg.Pivot)
	digest := permissionDigest(grants)
	approved, err := packagePermissionsApproved(store, installed, digest)
	if err != nil {
		return err
	}
	if len(grants) > 0 && !approved && !opts.AllowPermissions {
		return fmt.Errorf("%w for %s@%s: %s; rerun with --allow-permissions", ErrPackagePermissions, installed.Name, installed.Revision, strings.Join(grants, ", "))
	}

	preflight := func(generated map[string][]adapter.GeneratedFile, state *diff.StateFile, adapters map[string]adapter.Adapter) error {
		if err := rejectForeignPackageCollisions(pkg.Pivot, generated, state); err != nil {
			return err
		}
		// Record Managed right after the collision check succeeds, and before
		// any native write, so a crash mid-push never leaves the package
		// blocked on its own opencode.json entries: runPushAt persists state
		// (including this Managed record) before touching native files.
		recordPackageOpenCodeOwnership(pkg.Pivot, generated["opencode"], state)
		if len(grants) > 0 && !approved {
			return savePackageApproval(store, installed, digest)
		}
		return nil
	}
	postflight := func(generated map[string][]adapter.GeneratedFile, state *diff.StateFile) error {
		return nil
	}
	return runPushAt(filepath.Join(installed.Root, shenronpackage.PivotFileName), opts.Target, opts.Force, opts.Adapters, store.StateDir(installed.Name), format, preflight, postflight, output, os.Stderr)
}

// NewDiffCmd builds the top-level `diff` command.
func NewDiffCmd(store func() *shenronpackage.Store) *cobra.Command {
	var target, output string
	cmd := &cobra.Command{
		Use:          "diff <name>",
		Short:        "Show differences for an installed configuration package",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunPackageDiff(PackageDiffOptions{Store: store(), Name: args[0], Target: target, Format: output, Output: cmd.OutOrStdout()})
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "limit to a single CLI target (e.g. opencode)")
	cmd.Flags().StringVar(&output, "output", "text", "output format: text or json")
	return cmd
}

// NewPushCmd builds the top-level `push` command.
func NewPushCmd(store func() *shenronpackage.Store) *cobra.Command {
	var target, output string
	var force, allowPermissions bool
	cmd := &cobra.Command{
		Use:          "push <name>",
		Short:        "Push one installed configuration package",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunPackagePush(PackagePushOptions{Store: store(), Name: args[0], Target: target, Force: force, AllowPermissions: allowPermissions, Format: output, Output: cmd.OutOrStdout()})
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "limit to a single CLI target (e.g. opencode)")
	cmd.Flags().StringVar(&output, "output", "text", "output format: text or json")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite manually edited package-owned native files")
	cmd.Flags().BoolVar(&allowPermissions, "allow-permissions", false, "approve this package revision's declared permissions")
	return cmd
}

func missingPackageSkills(requirements shenronpackage.SkillRequirements, skillsDir string) (required, optional []string) {
	if skillsDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "~"
		}
		skillsDir = filepath.Join(home, ".agents", "skills")
	}
	for _, skill := range requirements.Required {
		if _, err := os.Stat(filepath.Join(skillsDir, skill, "SKILL.md")); err != nil {
			required = append(required, skill)
		}
	}
	for _, skill := range requirements.Optional {
		if _, err := os.Stat(filepath.Join(skillsDir, skill, "SKILL.md")); err != nil {
			optional = append(optional, skill)
		}
	}
	return required, optional
}

func printPackageRequirements(output io.Writer, grants, required, optional []string) error {
	if len(grants) == 0 {
		if _, err := fmt.Fprintln(output, "permissions: none requiring approval"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(output, "permissions requiring approval: %s\n", strings.Join(grants, ", ")); err != nil {
		return err
	}
	if len(required) > 0 {
		if _, err := fmt.Fprintf(output, "required skills unavailable: %s\n", strings.Join(required, ", ")); err != nil {
			return err
		}
	}
	if len(optional) > 0 {
		if _, err := fmt.Fprintf(output, "optional skills unavailable: %s\n", strings.Join(optional, ", ")); err != nil {
			return err
		}
	}
	return nil
}

func permissionGrants(pf *pivot.PivotFile) []string {
	var grants []string
	for _, agent := range pf.Agents {
		perms := agent.Permissions
		if perms == nil {
			continue
		}
		for _, field := range []struct {
			name  string
			value string
		}{{"edit", perms.Edit}, {"webfetch", perms.WebFetch}, {"websearch", perms.WebSearch}} {
			if isPermissionGrant(field.value) {
				grants = append(grants, agent.ID+"."+field.name+"="+field.value)
			}
		}
		grants = append(grants, bashPermissionGrants(agent.ID, perms.Bash)...)
		for task, value := range perms.Tasks {
			if isPermissionGrant(value) {
				grants = append(grants, agent.ID+".tasks."+task+"="+value)
			}
		}
	}
	sort.Strings(grants)
	return grants
}

func isPermissionGrant(value string) bool { return value != "" && value != "deny" }

func bashPermissionGrants(agentID string, value any) []string {
	var grants []string
	switch bash := value.(type) {
	case string:
		if isPermissionGrant(bash) {
			grants = append(grants, agentID+".bash="+bash)
		}
	case map[string]string:
		keys := sortedStringKeys(bash)
		for _, key := range keys {
			permission := bash[key]
			if isPermissionGrant(permission) {
				grants = append(grants, agentID+".bash."+key+"="+permission)
			}
		}
	case map[string]any:
		keys := sortedStringKeys(bash)
		for _, key := range keys {
			raw := bash[key]
			if permission, ok := raw.(string); ok && isPermissionGrant(permission) {
				grants = append(grants, agentID+".bash."+key+"="+permission)
			}
		}
	}
	return grants
}

func sortedStringKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func permissionDigest(grants []string) string {
	data, _ := json.Marshal(grants)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func packageApprovalPath(store *shenronpackage.Store, name string) string {
	return filepath.Join(store.StateDir(name), "permissions.json")
}

func packagePermissionsApproved(store *shenronpackage.Store, installed *shenronpackage.InstalledPackage, digest string) (bool, error) {
	data, err := os.ReadFile(packageApprovalPath(store, installed.Name))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read package permission approval: %w", err)
	}
	var approval packageApproval
	if err := json.Unmarshal(data, &approval); err != nil {
		return false, fmt.Errorf("parse package permission approval: %w", err)
	}
	return approval.Revision == installed.Revision && approval.PermissionDigest == digest, nil
}

func savePackageApproval(store *shenronpackage.Store, installed *shenronpackage.InstalledPackage, digest string) error {
	data, err := json.MarshalIndent(packageApproval{Revision: installed.Revision, PermissionDigest: digest}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal package permission approval: %w", err)
	}
	data = append(data, '\n')
	if err := fsutil.WriteFileAtomic(packageApprovalPath(store, installed.Name), data, 0o644); err != nil {
		return fmt.Errorf("write package permission approval: %w", err)
	}
	return nil
}

func rejectForeignPackageCollisions(pf *pivot.PivotFile, generated map[string][]adapter.GeneratedFile, state *diff.StateFile) error {
	for target, files := range generated {
		for _, f := range files {
			if target == "opencode" && filepath.Base(f.Path) == "opencode.json" {
				if err := rejectForeignOpenCodeCollisions(f.Path, pf, state); err != nil {
					return err
				}
				continue
			}
			if err := rejectForeignFileCollision(f.Path, state); err != nil {
				return err
			}
		}
	}
	return nil
}

func rejectForeignFileCollision(path string, state *diff.StateFile) error {
	if _, owned := state.Files[path]; owned {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%w: %s", ErrPackageCollision, path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect package output %s: %w", path, err)
	}
	return nil
}

func rejectForeignOpenCodeCollisions(path string, pf *pivot.PivotFile, state *diff.StateFile) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read OpenCode config %s: %w", path, err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse OpenCode config %s: %w", path, err)
	}
	owned := state.Managed(path)
	wanted := packageOpenCodeManaged(pf)
	for group, ids := range wanted {
		resources := map[string]json.RawMessage{}
		if raw, exists := root[group]; exists {
			if err := json.Unmarshal(raw, &resources); err != nil {
				return fmt.Errorf("parse OpenCode %s entries in %s: %w", group, path, err)
			}
		}
		for _, id := range ids {
			if _, exists := resources[id]; exists && !managedContains(owned, group, id) {
				return fmt.Errorf("%w: OpenCode %s.%s in %s", ErrPackageCollision, group, id, path)
			}
		}
	}
	return nil
}

func recordPackageOpenCodeOwnership(pf *pivot.PivotFile, files []adapter.GeneratedFile, state *diff.StateFile) {
	for _, f := range files {
		if filepath.Base(f.Path) == "opencode.json" {
			state.SetManaged(f.Path, packageOpenCodeManaged(pf))
		}
	}
}

func packageOpenCodeManaged(pf *pivot.PivotFile) map[string][]string {
	managed := map[string][]string{}
	for _, agent := range pf.Agents {
		managed["agent"] = appendUnique(managed["agent"], agent.ID)
	}
	for _, command := range pf.Commands {
		managed["command"] = appendUnique(managed["command"], command.ID)
	}
	for group := range managed {
		sort.Strings(managed[group])
	}
	return managed
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func managedContains(managed map[string][]string, group, id string) bool {
	for _, owned := range managed[group] {
		if owned == id {
			return true
		}
	}
	return false
}
