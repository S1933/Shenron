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

// RunPackageInstall installs a local directory or a public HTTPS Git package.
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

// NewPackageCmd creates the package command group.
func NewPackageCmd() *cobra.Command {
	var storeRoot string
	cmd := &cobra.Command{Use: "package", Short: "Install and manage standalone configuration packages"}
	cmd.PersistentFlags().StringVar(&storeRoot, "store", "", "package cache directory")

	var installRef string
	install := &cobra.Command{
		Use:   "install <source>",
		Short: "Install a local or public HTTPS Git package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunPackageInstall(PackageInstallOptions{Store: storeAt(storeRoot), Source: args[0], Ref: installRef, Output: cmd.OutOrStdout()})
		},
	}
	install.Flags().StringVar(&installRef, "ref", "", "immutable Git tag or full commit SHA (required for HTTPS sources)")

	list := &cobra.Command{
		Use:   "list",
		Short: "List installed configuration packages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunPackageList(PackageListOptions{Store: storeAt(storeRoot), Output: cmd.OutOrStdout()})
		},
	}

	var updateSource, updateRef string
	update := &cobra.Command{
		Use:   "update <name>",
		Short: "Validate and replace an installed package snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunPackageUpdate(PackageUpdateOptions{Store: storeAt(storeRoot), Name: args[0], Source: updateSource, Ref: updateRef, Output: cmd.OutOrStdout()})
		},
	}
	update.Flags().StringVar(&updateSource, "source", "", "replacement local directory or public HTTPS Git source")
	update.Flags().StringVar(&updateRef, "ref", "", "immutable Git tag or full commit SHA")

	cmd.AddCommand(install, list, update)
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
