// Package cli wires up okfi's cobra command tree.
package cli

import "github.com/spf13/cobra"

// NewRootCmd builds the okfi root command with all subcommands attached.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "okfi",
		Short: "Create and manage Google OKF bundles from data resources",
	}

	root.AddCommand(newGenerateCmd())

	return root
}
