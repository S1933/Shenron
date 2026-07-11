package main

import (
	"os"

	"github.com/S1933/Shenron/internal/cli"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "shenron",
		Short: "Install and manage standalone configuration packages",
	}

	rootCmd.AddCommand(cli.NewPackageCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
