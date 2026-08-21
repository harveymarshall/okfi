package cli

import "github.com/spf13/cobra"

// newGenerateCmd is the "generate" verb — parent of one subcommand per
// source (s3, and later gcs/bigquery/redshift).
func newGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate an OKF bundle from a data source",
	}

	cmd.AddCommand(newGenerateS3Cmd())

	return cmd
}
