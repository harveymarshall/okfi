package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newGenerateS3Cmd builds "okfi generate s3 <s3-uri> --out <dir>".
func newGenerateS3Cmd() *cobra.Command {
	var out string
	var region string
	var profile string

	cmd := &cobra.Command{
		Use:   "s3 <s3-uri>",
		Short: "Generate an OKF bundle from an S3 bucket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "generate s3 %s --out %s (not yet implemented)\n", args[0], out)
			return nil
		},
	}

	cmd.Flags().StringVar(&out, "out", "", "output directory for the generated bundle (required)")
	cmd.Flags().StringVar(&region, "region", "", "AWS region (optional, falls back to default credential chain)")
	cmd.Flags().StringVar(&profile, "profile", "", "AWS shared-config profile (optional)")
	_ = cmd.MarkFlagRequired("out")

	return cmd
}
