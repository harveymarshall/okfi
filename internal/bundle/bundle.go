// Package bundle writes OKF (Open Knowledge Format) bundle files: one
// markdown file per concept, with YAML frontmatter and a markdown body.
// It is source-agnostic — callers (s3walk, and future bigquery/gcs/redshift
// walkers) map their own data into a Concept before calling WriteBundle.
package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Concept is one OKF bundle file: a single documented unit (an S3 prefix,
// a BigQuery table, ...) identified by a dotted "<source>.<unit>" Type.
type Concept struct {
	// Slug names the output file (outDir/Slug + ".md"). Caller-computed so
	// each source controls its own filename scheme.
	Slug string
	// Type is the OKF concept type, e.g. "s3.prefix", "bigquery.table".
	Type      string
	Title     string
	Resource  string
	Timestamp time.Time
	// Body is the markdown body, written verbatim after the frontmatter.
	Body string
}

// WriteBundle writes one markdown file per concept into outDir, creating
// it if necessary. Re-running overwrites each concept's file in place —
// there is no merge/diff against prior content.
func WriteBundle(concepts []Concept, outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("bundle: create out dir: %w", err)
	}

	for _, c := range concepts {
		path := filepath.Join(outDir, c.Slug+".md")
		if err := os.WriteFile(path, []byte(render(c)), 0o644); err != nil {
			return fmt.Errorf("bundle: write %s: %w", path, err)
		}
	}
	return nil
}

func render(c Concept) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "type: %s\n", yamlString(c.Type))
	fmt.Fprintf(&b, "title: %s\n", yamlString(c.Title))
	fmt.Fprintf(&b, "resource: %s\n", yamlString(c.Resource))
	fmt.Fprintf(&b, "timestamp: %s\n", yamlString(c.Timestamp.UTC().Format(time.RFC3339)))
	// description is intentionally always blank — left for a human/LLM to
	// fill in later, never auto-generated (see grilling decision Q6).
	b.WriteString("description: \"\"\n")
	b.WriteString("---\n\n")
	b.WriteString(c.Body)
	return b.String()
}

// yamlString renders s as a double-quoted YAML scalar, safe regardless of
// content (colons, hashes, etc. in prefix paths or resource URIs).
func yamlString(s string) string {
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
