package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jnuel/agentsync/internal/fsutil"
	"github.com/jnuel/agentsync/internal/pivot"
	"github.com/spf13/cobra"
)

// NewPushCmd creates the push subcommand.
func NewPushCmd(configPath *string) *cobra.Command {
	var target string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push pivot config to native CLI configs",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRun {
				return runDiff(*configPath, target)
			}
			return runPush(*configPath, target)
		},
	}

	cmd.Flags().StringVar(&target, "target", "", "limit to a single CLI target (e.g. opencode)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show changes without writing")
	return cmd
}

func runPush(configPath, target string) error {
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

	for name, files := range generated {
		changes, hasChanges := compareFiles(files)
		if !hasChanges {
			fmt.Printf("[%s] No changes\n", name)
			continue
		}

		for _, c := range changes {
			if c.status == "unchanged" {
				continue
			}
			content := files[c.path]
			if err := fsutil.WriteFileAtomic(c.path, []byte(content), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", c.path, err)
			}
			fmt.Printf("[%s] wrote %s (%s)\n", name, c.path, c.status)
		}
	}

	return nil
}
