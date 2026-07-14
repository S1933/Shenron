package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	shenronpackage "github.com/S1933/Shenron/internal/package"
	"github.com/spf13/cobra"
)

// PackageInstallOptions makes package installation independently testable.
type PackageInstallOptions struct {
	Store  *shenronpackage.Store
	Source string
	Ref    string
	Output io.Writer
}

// PackageListOptions makes package listing independently testable.
type PackageListOptions struct {
	Store  *shenronpackage.Store
	Output io.Writer
}

// PackageUpdateOptions makes package updates independently testable. Leaving
// Source or Ref empty reuses the corresponding installed value.
type PackageUpdateOptions struct {
	Store  *shenronpackage.Store
	Name   string
	Source string
	Ref    string
	Output io.Writer
}

// RunPackageInstall installs a local directory or a remote (HTTPS or SSH) Git package.
func RunPackageInstall(opts PackageInstallOptions) error {
	store := packageStore(opts.Store)
	installed, err := store.Install(opts.Source, opts.Ref)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(packageOutput(opts.Output), "installed package %s@%s (%s)\n", installed.Name, installed.Version, installed.Revision)
	return err
}

// RunPackageList prints installed package records ordered by name.
func RunPackageList(opts PackageListOptions) error {
	packages, err := packageStore(opts.Store).List()
	if err != nil {
		return err
	}
	output := packageOutput(opts.Output)
	if len(packages) == 0 {
		_, err := fmt.Fprintln(output, "No packages installed")
		return err
	}
	for _, installed := range packages {
		if _, err := fmt.Fprintf(output, "%s\t%s\t%s\t%s\n", installed.Name, installed.Version, installed.Source, installed.Revision); err != nil {
			return err
		}
	}
	return nil
}

// RunPackageUpdate replaces one package's active snapshot only after its new
// source has been fetched and validated.
func RunPackageUpdate(opts PackageUpdateOptions) error {
	store := packageStore(opts.Store)
	packages, err := store.List()
	if err != nil {
		return err
	}
	var current *shenronpackage.InstalledPackage
	for i := range packages {
		if packages[i].Name == opts.Name {
			current = &packages[i]
			break
		}
	}
	if current == nil {
		return fmt.Errorf("package %q is not installed", opts.Name)
	}
	source := opts.Source
	if source == "" {
		source = current.Source
	}
	ref := opts.Ref
	if ref == "" {
		ref = current.Ref
	}
	installed, err := store.Update(opts.Name, source, ref)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(packageOutput(opts.Output), "updated package %s@%s (%s)\n", installed.Name, installed.Version, installed.Revision)
	return err
}

// NewRootCmd builds the top-level shenron command with the persistent
// --store flag and the five top-level subcommands. The store flag is
// read lazily through a resolver so subcommand RunE handlers see the
// value Cobra parsed for the root.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "shenron",
		Short:        "Install and manage standalone configuration packages",
		SilenceUsage: true,
	}
	cmd.PersistentFlags().String("store", "", "package cache directory (default ~/.shenron/packages)")

	resolver := func() *shenronpackage.Store {
		root, _ := cmd.PersistentFlags().GetString("store")
		return storeAt(root)
	}

	cmd.AddCommand(
		NewInstallCmd(resolver),
		NewListCmd(resolver),
		NewUpdateCmd(resolver),
		NewDiffCmd(resolver),
		NewPushCmd(resolver),
		NewDoctorCmd(resolver),
		NewExplainCmd(resolver),
	)
	return cmd
}

// NewExplainCmd builds the top-level `explain` command.
func NewExplainCmd(store func() *shenronpackage.Store) *cobra.Command {
	var target, output string
	cmd := &cobra.Command{
		Use:          "explain <name>",
		Short:        "Preview the native files a package translates into for a target",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunExplain(ExplainOptions{Store: store(), Name: args[0], Target: target, Format: output, Output: cmd.OutOrStdout()})
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "CLI target to explain: claude-code, codex, or opencode (required)")
	cmd.Flags().StringVar(&output, "output", "text", "output format: text or json")
	return cmd
}

// NewDoctorCmd builds the top-level `doctor` command.
func NewDoctorCmd(store func() *shenronpackage.Store) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:          "doctor",
		Short:        "Check tool paths, snapshot cache, state, and permission approvals",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunDoctor(DoctorOptions{Store: store(), Format: output, Output: cmd.OutOrStdout()})
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "output format: text or json")
	return cmd
}

// NewInstallCmd builds the top-level `install` command.
func NewInstallCmd(store func() *shenronpackage.Store) *cobra.Command {
	var ref string
	cmd := &cobra.Command{
		Use:          "install <source>",
		Short:        "Install a local, public HTTPS, or SSH Git package",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunPackageInstall(PackageInstallOptions{Store: store(), Source: args[0], Ref: ref, Output: cmd.OutOrStdout()})
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "immutable Git tag or full commit SHA (required for HTTPS and SSH sources)")
	return cmd
}

// NewListCmd builds the top-level `list` command.
func NewListCmd(store func() *shenronpackage.Store) *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List installed configuration packages",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunPackageList(PackageListOptions{Store: store(), Output: cmd.OutOrStdout()})
		},
	}
}

// NewUpdateCmd builds the top-level `update` command.
func NewUpdateCmd(store func() *shenronpackage.Store) *cobra.Command {
	var source, ref string
	cmd := &cobra.Command{
		Use:          "update <name>",
		Short:        "Validate and replace an installed package snapshot",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunPackageUpdate(PackageUpdateOptions{Store: store(), Name: args[0], Source: source, Ref: ref, Output: cmd.OutOrStdout()})
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "replacement local directory or remote (HTTPS or SSH) Git source")
	cmd.Flags().StringVar(&ref, "ref", "", "immutable Git tag or full commit SHA")
	return cmd
}

func packageStore(store *shenronpackage.Store) *shenronpackage.Store {
	if store != nil {
		return store
	}
	return storeAt("")
}

func packageOutput(output io.Writer) io.Writer {
	if output != nil {
		return output
	}
	return os.Stdout
}

func storeAt(root string) *shenronpackage.Store {
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return shenronpackage.NewStore(filepath.Join("~", ".shenron", "packages"))
		}
		root = filepath.Join(home, ".shenron", "packages")
	}
	return shenronpackage.NewStore(root)
}
