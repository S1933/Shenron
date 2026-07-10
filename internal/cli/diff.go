package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jnuel/agentsync/internal/pivot"
	"github.com/spf13/cobra"
)

// NewDiffCmd creates the diff subcommand.
func NewDiffCmd(configPath *string) *cobra.Command {
	var target string

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show differences between pivot and native configs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(*configPath, target)
		},
	}

	cmd.Flags().StringVar(&target, "target", "", "limit to a single CLI target (e.g. opencode)")
	return cmd
}

func runDiff(configPath, target string) error {
	path, err := pivot.Discover(configPath)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read pivot file: %w", err)
	}

	pivotDir := filepath.Dir(path)
	pf, err := pivot.Parse(data, pivotDir)
	if err != nil {
		return err
	}

	adapters, err := ResolveTargets(target)
	if err != nil {
		return err
	}

	generated, err := Generate(pf, pivotDir, adapters)
	if err != nil {
		return err
	}

	hasChanges := false
	for name, files := range generated {
		fmt.Printf("[%s]\n", name)
		changes, changed := compareFiles(files)
		printDiffReport(changes, changed)
		if changed {
			hasChanges = true
		}
		fmt.Println()
	}

	if !hasChanges {
		fmt.Println("No changes")
	}

	return nil
}
