package cli

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"

	"github.com/harveymarshall/okfi/internal/bundle"
	"github.com/harveymarshall/okfi/internal/s3walk"
)

// defaultObjectTypes is the extension allowlist applied when --object-types
// is not given: document-ish files worth grounding an agent on. Bulk /
// partitioned data (csv, json, parquet, ...) is deliberately excluded — it
// stays rolled into each prefix's index.md summary.
var defaultObjectTypes = []string{"pdf", "md", "markdown", "txt", "rst", "docx", "doc", "xlsx", "pptx"}

// newGenerateS3Cmd builds "okfi generate s3 <s3-uri> --out <dir>".
func newGenerateS3Cmd() *cobra.Command {
	var out string
	var region string
	var profile string
	var objectTypes []string

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

			allow := newExtensionFilter(objectTypes)

			summaries, err := s3walk.WalkPrefixes(ctx, client, bucket, rootPrefix, allow)
			if err != nil {
				return fmt.Errorf("walk s3://%s/%s: %w", bucket, rootPrefix, err)
			}

			now := time.Now()

			var concepts []bundle.Concept
			var dirSummaries []bundle.DirSummary
			for _, s := range summaries {
				dirSummaries = append(dirSummaries, toDirSummary(s))
				for _, obj := range s.Objects {
					concepts = append(concepts, toObjectConcept(bucket, obj, now))
				}
			}

			if err := bundle.WriteBundle(concepts, dirSummaries, out); err != nil {
				return fmt.Errorf("write bundle: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "wrote %d concept(s) to %s\n", len(concepts), out)
			return nil
		},
	}

	cmd.Flags().StringVar(&out, "out", "", "output directory for the generated bundle (required)")
	cmd.Flags().StringVar(&region, "region", "", "AWS region (optional, falls back to default credential chain)")
	cmd.Flags().StringVar(&profile, "profile", "", "AWS shared-config profile (optional)")
	cmd.Flags().StringSliceVar(&objectTypes, "object-types", defaultObjectTypes,
		`file extensions to promote to concepts, comma-separated (e.g. "pdf,md"); "*" promotes every object`)
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

// newExtensionFilter returns a predicate reporting whether an object key
// should be promoted to a concept, given the --object-types allowlist. A
// list containing "*" promotes every object; otherwise the key's final
// extension is matched case-insensitively, with or without a leading dot.
// Directory-marker keys (trailing "/") are never promoted, and blank
// allowlist entries are ignored so they can't match extensionless keys.
func newExtensionFilter(objectTypes []string) func(key string) bool {
	star := false
	allow := make(map[string]bool, len(objectTypes))
	for _, t := range objectTypes {
		if t == "*" {
			star = true
			continue
		}
		if ext := normalizeExt(t); ext != "" {
			allow[ext] = true
		}
	}
	return func(key string) bool {
		if key == "" || strings.HasSuffix(key, "/") {
			return false
		}
		if star {
			return true
		}
		return allow[normalizeExt(path.Ext(path.Base(key)))]
	}
}

func normalizeExt(s string) string {
	return strings.ToLower(strings.TrimPrefix(s, "."))
}

// toObjectConcept maps a directly-listed S3 object onto an OKF concept. The
// slug keeps the full object key (separators and extension), so
// bundle.WriteBundle lays the concept out at an output path mirroring the
// source key: "reports/2026/q1.pdf" -> "reports/2026/q1.pdf.md".
func toObjectConcept(bucket string, o s3walk.ObjectInfo, generatedAt time.Time) bundle.Concept {
	return bundle.Concept{
		Slug:      o.Key,
		Type:      "s3.object",
		Title:     path.Base(o.Key),
		Resource:  fmt.Sprintf("s3://%s/%s", bucket, o.Key),
		Timestamp: generatedAt,
		Body:      renderObjectBody(o),
	}
}

func renderObjectBody(o s3walk.ObjectInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- Key: %s\n", o.Key)
	fmt.Fprintf(&b, "- Size: %d bytes\n", o.Size)
	if !o.LastModified.IsZero() {
		fmt.Fprintf(&b, "- Last modified: %s\n", o.LastModified.UTC().Format(time.RFC3339))
	}
	return b.String()
}

// toDirSummary maps a walker's per-level prefix summary onto the
// directory-level aggregate rendered into that level's index.md. The slug
// drops the surrounding slashes so it lines up with the bundle's directory
// tree ("" is the bundle root).
func toDirSummary(s s3walk.PrefixSummary) bundle.DirSummary {
	return bundle.DirSummary{
		Slug:        strings.Trim(s.Prefix, "/"),
		ObjectCount: s.ObjectCount,
		TotalSize:   s.TotalSize,
		MinModified: s.MinModified,
		MaxModified: s.MaxModified,
	}
}
