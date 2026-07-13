package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/S1933/Shenron/internal/diff"
	"github.com/S1933/Shenron/internal/fsutil"
	shenronpackage "github.com/S1933/Shenron/internal/package"
)

// ErrDoctorFailed is returned when at least one health check fails, so the
// process exits non-zero.
var ErrDoctorFailed = errors.New("doctor found problems")

// Check status tokens.
const (
	statusOK   = "ok"
	statusWarn = "warn"
	statusFail = "fail"
)

// CheckResult is one health-check outcome.
type CheckResult struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | warn | fail
	Detail string `json:"detail,omitempty"`
}

// DoctorReport is the aggregate health report (also the JSON shape).
type DoctorReport struct {
	Checks []CheckResult `json:"checks"`
	OK     bool          `json:"ok"` // false if any check failed (warnings do not count)
}

// DoctorOptions configures the doctor command for tests and embedding.
type DoctorOptions struct {
	Store  *shenronpackage.Store
	Format string // "text" (default) or "json"
	Output io.Writer
}

// RunDoctor inspects the environment and every installed package, reporting
// tool paths, snapshot-cache integrity, sync state, and pending permission
// approvals. It returns ErrDoctorFailed if any check has status "fail".
func RunDoctor(opts DoctorOptions) error {
	format, err := parseOutputFormat(opts.Format)
	if err != nil {
		return err
	}
	store := packageStore(opts.Store)
	output := packageOutput(opts.Output)

	var checks []CheckResult
	checks = append(checks, checkTargetPaths()...)
	checks = append(checks, checkPackages(store)...)

	report := DoctorReport{Checks: checks, OK: true}
	for _, c := range checks {
		if c.Status == statusFail {
			report.OK = false
		}
	}

	if format == formatJSON {
		if err := writeJSON(output, report); err != nil {
			return err
		}
	} else {
		for _, c := range report.Checks {
			if _, err := fmt.Fprintf(output, "[%s] %s: %s\n", c.Status, c.Name, c.Detail); err != nil {
				return err
			}
		}
	}

	if !report.OK {
		return ErrDoctorFailed
	}
	return nil
}

// checkTargetPaths reports each adapter's config directory and whether its
// nearest existing ancestor is writable. A missing directory is fine (push
// creates it); an unwritable ancestor is a warning.
func checkTargetPaths() []CheckResult {
	targets := []struct {
		name string
		path string
	}{
		{"claude-code", fsutil.ClaudePath()},
		{"codex", fsutil.CodexPath()},
		{"opencode", fsutil.OpenCodePath()},
	}

	checks := make([]CheckResult, 0, len(targets))
	for _, t := range targets {
		name := "target " + t.name
		if writableAncestor(t.path) {
			checks = append(checks, CheckResult{Name: name, Status: statusOK, Detail: t.path})
		} else {
			checks = append(checks, CheckResult{Name: name, Status: statusWarn, Detail: t.path + " (not writable)"})
		}
	}
	return checks
}

// writableAncestor reports whether the nearest existing ancestor of path can be
// written to, by staging and removing a temp entry there.
func writableAncestor(path string) bool {
	dir := path
	for {
		if _, err := os.Stat(dir); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
	tmp, err := os.MkdirTemp(dir, ".shenron-doctor-*")
	if err != nil {
		return false
	}
	_ = os.Remove(tmp)
	return true
}

// checkPackages validates every installed package's snapshot, state, and
// permission-approval status.
func checkPackages(store *shenronpackage.Store) []CheckResult {
	installed, err := store.List()
	if err != nil {
		return []CheckResult{{Name: "packages", Status: statusFail, Detail: err.Error()}}
	}
	if len(installed) == 0 {
		return []CheckResult{{Name: "packages", Status: statusOK, Detail: "no packages installed"}}
	}

	var checks []CheckResult
	for _, p := range installed {
		checks = append(checks, checkPackage(store, p))
	}
	return checks
}

// checkPackage runs the per-package checks and folds them into a single result:
// the snapshot digest is rechecked (cache integrity), the state file must parse,
// and declared permissions must be approved for the installed revision.
func checkPackage(store *shenronpackage.Store, p shenronpackage.InstalledPackage) CheckResult {
	name := fmt.Sprintf("package %s@%s", p.Name, p.Version)

	installed, pkg, err := store.Load(p.Name)
	if err != nil {
		return CheckResult{Name: name, Status: statusFail, Detail: "snapshot invalid: " + err.Error()}
	}

	if _, err := diff.LoadState(store.StateDir(p.Name)); err != nil {
		return CheckResult{Name: name, Status: statusFail, Detail: "state unreadable: " + err.Error()}
	}

	grants := permissionGrants(pkg.Pivot)
	if len(grants) > 0 {
		approved, err := packagePermissionsApproved(store, installed, permissionDigest(grants))
		if err != nil {
			return CheckResult{Name: name, Status: statusFail, Detail: "approval unreadable: " + err.Error()}
		}
		if !approved {
			return CheckResult{Name: name, Status: statusWarn, Detail: "permission approval pending; push requires --allow-permissions"}
		}
	}

	return CheckResult{Name: name, Status: statusOK, Detail: "snapshot verified, state and permissions ok"}
}
