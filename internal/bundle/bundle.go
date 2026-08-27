// Package bundle writes OKF (Open Knowledge Format) bundle files: one
// markdown file per concept, with YAML frontmatter and a markdown body,
// plus an index.md at every directory level carrying that level's summary
// and links to its children. It is source-agnostic — callers (s3walk, and
// future bigquery/gcs/redshift walkers) map their own data into Concepts
// and DirSummaries before calling WriteBundle.
package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Concept is one OKF bundle file: a single documented unit (an S3 object,
// a BigQuery table, ...) identified by a dotted "<source>.<unit>" Type.
type Concept struct {
	// Slug names the output file (outDir/Slug + ".md") and its position in
	// the directory tree. Caller-computed so each source controls its own
	// filename scheme.
	Slug string
	// Type is the OKF concept type, e.g. "s3.object", "bigquery.table".
	Type      string
	Title     string
	Resource  string
	Timestamp time.Time
	// Body is the markdown body, written verbatim after the frontmatter.
	Body string
}

// DirSummary is one directory level's aggregate, rendered into that level's
// index.md as a "## Summary" section. Slug is the directory path relative
// to the bundle root ("" is the root itself), e.g. "a/b".
type DirSummary struct {
	Slug        string
	ObjectCount int
	TotalSize   int64
	MinModified time.Time
	MaxModified time.Time
}

// WriteBundle writes one markdown file per concept into outDir, creating it
// if necessary, plus an index.md at every directory level implied by the
// concepts and summaries. Re-running overwrites each file in place and
// rebuilds every index.md from scratch — there is no merge/diff against
// prior content, and index.md files never carry stale links from a prior
// run.
func WriteBundle(concepts []Concept, summaries []DirSummary, outDir string) error {
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

	return writeIndexes(concepts, summaries, outDir)
}

// dirNode is one directory level in the concept tree used to render
// index.md files: the concepts filed directly under it, its immediate
// child subdirectories, and (if the caller supplied one) that level's
// summary.
type dirNode struct {
	concepts []Concept
	subdirs  map[string]*dirNode
	summary  *DirSummary
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

// descend walks (creating as needed) the chain of subdirectories named by
// parts, returning the node at the end.
func (n *dirNode) descend(parts []string) *dirNode {
	node := n
	for _, part := range parts {
		node = node.child(part)
	}
	return node
}

// splitSlug splits a "/"-separated slug into path segments, dropping empty
// ones so a leading, trailing, or doubled slash in an object key can't file
// content under an empty-named node (which resolves back to the bundle
// root and would clobber the root index.md).
func splitSlug(slug string) []string {
	raw := strings.Split(slug, "/")
	parts := raw[:0]
	for _, p := range raw {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// dirParts is splitSlug minus the final segment: the directory chain a
// concept file lives in.
func dirParts(slug string) []string {
	parts := splitSlug(slug)
	if len(parts) == 0 {
		return nil
	}
	return parts[:len(parts)-1]
}

// writeIndexes builds the directory tree implied by concepts' slugs and
// summaries' slugs, then writes an index.md at outDir and every nested
// subdirectory: each level's "## Summary" (when supplied), its direct child
// concepts, and its child subdirectories. A level can have a summary and no
// concepts (a bulk-data prefix) and still gets an index.md. It is a full
// rebuild on every call.
func writeIndexes(concepts []Concept, summaries []DirSummary, outDir string) error {
	root := newDirNode()

	for _, c := range concepts {
		node := root.descend(dirParts(c.Slug))
		node.concepts = append(node.concepts, c)
	}

	for _, s := range summaries {
		summary := s
		root.descend(splitSlug(s.Slug)).summary = &summary
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

	if node.summary != nil {
		b.WriteString("## Summary\n\n")
		fmt.Fprintf(&b, "- Object count: %d\n", node.summary.ObjectCount)
		fmt.Fprintf(&b, "- Total size: %d bytes\n", node.summary.TotalSize)
		if !node.summary.MinModified.IsZero() || !node.summary.MaxModified.IsZero() {
			fmt.Fprintf(&b, "- Last modified range: %s to %s\n",
				node.summary.MinModified.UTC().Format(time.RFC3339),
				node.summary.MaxModified.UTC().Format(time.RFC3339))
		}
		b.WriteString("\n")
	}

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
