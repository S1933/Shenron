package main

import (
	"os"

	"github.com/S1933/Shenron/internal/cli"
	"github.com/spf13/cobra"
)

var configPath string

func main() {
	rootCmd := &cobra.Command{
		Use:   "shenron",
		Short: "Sync agent configurations across AI coding assistants",
	}

	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "path to shenron.yaml pivot file")

	rootCmd.AddCommand(
		cli.NewDiffCmd(&configPath),
		cli.NewPushCmd(&configPath),
		cli.NewValidateCmd(&configPath),
		cli.NewInitCmd(),
		cli.NewPackageCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
