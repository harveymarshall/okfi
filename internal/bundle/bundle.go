// Package bundle writes OKF (Open Knowledge Format) bundle files: one
// markdown file per concept, with YAML frontmatter and a markdown body.
// It is source-agnostic — callers (s3walk, and future bigquery/gcs/redshift
// walkers) map their own data into a Concept before calling WriteBundle.
package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("bundle: create dir for %s: %w", path, err)
		}
		if err := os.WriteFile(path, []byte(render(c)), 0o644); err != nil {
			return fmt.Errorf("bundle: write %s: %w", path, err)
		}
	}

	if err := writeIndexes(concepts, outDir); err != nil {
		return err
	}
	return nil
}

// dirNode is one directory level in the concept tree used to render
// index.md files: the concepts filed directly under it, and its immediate
// child subdirectories.
type dirNode struct {
	concepts []Concept
	subdirs  map[string]*dirNode
}

func newDirNode() *dirNode {
	return &dirNode{subdirs: map[string]*dirNode{}}
}

func (n *dirNode) child(name string) *dirNode {
	c, ok := n.subdirs[name]
	if !ok {
		c = newDirNode()
		n.subdirs[name] = c
	}
	return c
}

// writeIndexes builds the directory tree implied by concepts' slugs and
// writes an index.md at outDir (the bundle root) and every nested
// subdirectory, each listing its direct child concepts and subdirectories.
// It is a full rebuild from the current concepts on every call, so
// re-running WriteBundle regenerates each index.md from scratch — no stale
// links survive from a prior run.
func writeIndexes(concepts []Concept, outDir string) error {
	root := newDirNode()
	for _, c := range concepts {
		parts := strings.Split(c.Slug, "/")
		node := root
		for _, part := range parts[:len(parts)-1] {
			node = node.child(part)
		}
		node.concepts = append(node.concepts, c)
	}
	return writeIndex(root, outDir)
}

func writeIndex(node *dirNode, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("bundle: create dir %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(renderIndex(node)), 0o644); err != nil {
		return fmt.Errorf("bundle: write index for %s: %w", dir, err)
	}

	names := make([]string, 0, len(node.subdirs))
	for name := range node.subdirs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := writeIndex(node.subdirs[name], filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

func renderIndex(node *dirNode) string {
	concepts := append([]Concept(nil), node.concepts...)
	sort.Slice(concepts, func(i, j int) bool { return concepts[i].Slug < concepts[j].Slug })

	subdirs := make([]string, 0, len(node.subdirs))
	for name := range node.subdirs {
		subdirs = append(subdirs, name)
	}
	sort.Strings(subdirs)

	var b strings.Builder
	b.WriteString("# Index\n\n")

	if len(concepts) > 0 {
		b.WriteString("## Concepts\n\n")
		for _, c := range concepts {
			base := filepath.Base(c.Slug)
			title := c.Title
			if title == "" {
				title = base
			}
			fmt.Fprintf(&b, "- [%s](%s.md)\n", title, base)
		}
		b.WriteString("\n")
	}

	if len(subdirs) > 0 {
		b.WriteString("## Directories\n\n")
		for _, name := range subdirs {
			fmt.Fprintf(&b, "- [%s/](%s/index.md)\n", name, name)
		}
	}

	return b.String()
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
