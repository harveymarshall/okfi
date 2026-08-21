package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"

	"github.com/harveymarshall/okfi/internal/bundle"
	"github.com/harveymarshall/okfi/internal/s3walk"
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
			bucket, rootPrefix, err := parseS3URI(args[0])
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			cfg, err := loadAWSConfig(ctx, region, profile)
			if err != nil {
				return fmt.Errorf("load AWS config: %w", err)
			}
			client := s3.NewFromConfig(cfg)

			summaries, err := s3walk.WalkPrefixes(ctx, client, bucket, rootPrefix)
			if err != nil {
				return fmt.Errorf("walk s3://%s/%s: %w", bucket, rootPrefix, err)
			}

			concepts := make([]bundle.Concept, 0, len(summaries))
			for _, s := range summaries {
				concepts = append(concepts, toConcept(bucket, s, time.Now()))
			}

			if err := bundle.WriteBundle(concepts, out); err != nil {
				return fmt.Errorf("write bundle: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "wrote %d concept(s) to %s\n", len(concepts), out)
			return nil
		},
	}

	cmd.Flags().StringVar(&out, "out", "", "output directory for the generated bundle (required)")
	cmd.Flags().StringVar(&region, "region", "", "AWS region (optional, falls back to default credential chain)")
	cmd.Flags().StringVar(&profile, "profile", "", "AWS shared-config profile (optional)")
	_ = cmd.MarkFlagRequired("out")

	return cmd
}

// loadAWSConfig loads the AWS SDK default credential chain (env vars,
// shared config/credentials file, IAM role), optionally overriding region
// and profile. No custom credential flags — see grilling decision.
func loadAWSConfig(ctx context.Context, region, profile string) (aws.Config, error) {
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	return config.LoadDefaultConfig(ctx, opts...)
}

// parseS3URI splits "s3://bucket/prefix" into bucket and prefix. A missing
// prefix (bare "s3://bucket") yields an empty rootPrefix, walking the whole
// bucket.
func parseS3URI(uri string) (bucket, prefix string, err error) {
	const schemePrefix = "s3://"
	if !strings.HasPrefix(uri, schemePrefix) {
		return "", "", fmt.Errorf("invalid s3 URI %q: must start with %q", uri, schemePrefix)
	}
	rest := strings.TrimPrefix(uri, schemePrefix)
	if rest == "" {
		return "", "", fmt.Errorf("invalid s3 URI %q: missing bucket", uri)
	}

	bucket, prefix, _ = strings.Cut(rest, "/")
	if bucket == "" {
		return "", "", fmt.Errorf("invalid s3 URI %q: missing bucket", uri)
	}

	// Normalize with a trailing slash so S3's plain string-prefix match
	// only matches true descendants (e.g. "some/path/foo"), not unrelated
	// siblings that merely share the string prefix (e.g. "some/path-other/").
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return bucket, prefix, nil
}

// toConcept maps a walker's shallow prefix summary onto an OKF concept. The
// slug keeps the prefix's "/" separators (rather than flattening them) so
// bundle.WriteBundle lays nested prefixes out at matching nested output
// paths, mirroring the source hierarchy.
func toConcept(bucket string, s s3walk.PrefixSummary, generatedAt time.Time) bundle.Concept {
	slug := strings.Trim(s.Prefix, "/")
	if slug == "" {
		slug = "root"
	}

	return bundle.Concept{
		Slug:      slug,
		Type:      "s3.prefix",
		Title:     s.Prefix,
		Resource:  fmt.Sprintf("s3://%s/%s", bucket, s.Prefix),
		Timestamp: generatedAt,
		Body:      renderBody(s),
	}
}

func renderBody(s s3walk.PrefixSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- Object count: %d\n", s.ObjectCount)
	fmt.Fprintf(&b, "- Total size: %d bytes\n", s.TotalSize)
	if !s.MinModified.IsZero() || !s.MaxModified.IsZero() {
		fmt.Fprintf(&b, "- Last modified range: %s to %s\n",
			s.MinModified.UTC().Format(time.RFC3339), s.MaxModified.UTC().Format(time.RFC3339))
	}
	if len(s.SampleKeys) > 0 {
		b.WriteString("- Sample keys:\n")
		for _, key := range s.SampleKeys {
			fmt.Fprintf(&b, "  - %s\n", key)
		}
	}
	return b.String()
}
