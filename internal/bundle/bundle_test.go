package bundle_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/harveymarshall/okfi/internal/bundle"
)

func TestWriteBundle_WritesOneFilePerConcept(t *testing.T) {
	outDir := t.TempDir()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	concepts := []bundle.Concept{
		{
			Slug:      "orders/2026.csv",
			Type:      "s3.object",
			Title:     "2026.csv",
			Resource:  "s3://my-bucket/orders/2026.csv",
			Timestamp: ts,
			Body:      "- Size: 300 bytes\n",
		},
		{
			Slug:      "notes.md",
			Type:      "s3.object",
			Title:     "notes.md",
			Resource:  "s3://my-bucket/notes.md",
			Timestamp: ts,
			Body:      "- Size: 10 bytes\n",
		},
	}

	if err := bundle.WriteBundle(concepts, nil, outDir); err != nil {
		t.Fatalf("WriteBundle returned error: %v", err)
	}

	for _, rel := range []string{"orders/2026.csv.md", "notes.md.md", "index.md", "orders/index.md"} {
		if _, err := os.Stat(filepath.Join(outDir, rel)); err != nil {
			t.Errorf("expected file %s to exist: %v", rel, err)
		}
	}
}

func TestWriteBundle_FrontmatterAndBody(t *testing.T) {
	outDir := t.TempDir()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	concepts := []bundle.Concept{{
		Slug:      "report.pdf",
		Type:      "s3.object",
		Title:     "report.pdf",
		Resource:  "s3://my-bucket/report.pdf",
		Timestamp: ts,
		Body:      "- Size: 300 bytes\n",
	}}

	if err := bundle.WriteBundle(concepts, nil, outDir); err != nil {
		t.Fatalf("WriteBundle returned error: %v", err)
	}

	content := readFile(t, filepath.Join(outDir, "report.pdf.md"))

	if !strings.HasPrefix(content, "---\n") {
		t.Errorf("content does not start with frontmatter delimiter, got: %q", content)
	}
	for _, want := range []string{
		`type: "s3.object"`,
		`title: "report.pdf"`,
		`resource: "s3://my-bucket/report.pdf"`,
		`timestamp: "2026-01-01T12:00:00Z"`,
		`description: ""`,
		"- Size: 300 bytes",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("content missing %q, got:\n%s", want, content)
		}
	}

	if strings.Index(content[4:], "---\n") == -1 {
		t.Fatalf("no closing frontmatter delimiter found")
	}
}

func TestWriteBundle_NestedSlugCreatesSubdirectories(t *testing.T) {
	outDir := t.TempDir()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	concepts := []bundle.Concept{{
		Slug:      "a/b/baz.pdf",
		Type:      "s3.object",
		Title:     "baz.pdf",
		Resource:  "s3://my-bucket/a/b/baz.pdf",
		Timestamp: ts,
		Body:      "- Size: 1 bytes\n",
	}}

	if err := bundle.WriteBundle(concepts, nil, outDir); err != nil {
		t.Fatalf("WriteBundle returned error: %v", err)
	}

	wantPath := filepath.Join(outDir, "a", "b", "baz.pdf.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected nested file %s to exist: %v", wantPath, err)
	}
}

func TestWriteBundle_RootIndexLinksConceptsAndDirs(t *testing.T) {
	outDir := t.TempDir()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	concepts := []bundle.Concept{
		{Slug: "top.pdf", Type: "s3.object", Title: "top.pdf", Resource: "s3://b/top.pdf", Timestamp: ts, Body: "x\n"},
		{Slug: "orders/o.pdf", Type: "s3.object", Title: "o.pdf", Resource: "s3://b/orders/o.pdf", Timestamp: ts, Body: "x\n"},
	}

	if err := bundle.WriteBundle(concepts, nil, outDir); err != nil {
		t.Fatalf("WriteBundle returned error: %v", err)
	}

	content := readFile(t, filepath.Join(outDir, "index.md"))
	if !strings.Contains(content, "top.pdf.md") {
		t.Errorf("root index.md missing link to top.pdf.md, got:\n%s", content)
	}
	if !strings.Contains(content, "orders/index.md") {
		t.Errorf("root index.md missing link to child directory orders/, got:\n%s", content)
	}
}

func TestWriteBundle_SummarySectionRendersFirst(t *testing.T) {
	outDir := t.TempDir()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	mod := time.Date(2026, 8, 22, 14, 39, 12, 0, time.UTC)

	concepts := []bundle.Concept{
		{Slug: "report.pdf", Type: "s3.object", Title: "report.pdf", Resource: "s3://b/report.pdf", Timestamp: ts, Body: "x\n"},
	}
	summaries := []bundle.DirSummary{
		{Slug: "", ObjectCount: 3, TotalSize: 198150, MinModified: mod, MaxModified: mod},
	}

	if err := bundle.WriteBundle(concepts, summaries, outDir); err != nil {
		t.Fatalf("WriteBundle returned error: %v", err)
	}

	content := readFile(t, filepath.Join(outDir, "index.md"))
	for _, want := range []string{
		"## Summary",
		"- Object count: 3",
		"- Total size: 198150 bytes",
		"- Last modified range: 2026-08-22T14:39:12Z to 2026-08-22T14:39:12Z",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("index.md missing %q, got:\n%s", want, content)
		}
	}

	summaryAt := strings.Index(content, "## Summary")
	conceptsAt := strings.Index(content, "## Concepts")
	if summaryAt == -1 || conceptsAt == -1 || summaryAt > conceptsAt {
		t.Errorf("expected ## Summary before ## Concepts, got:\n%s", content)
	}
}

func TestWriteBundle_BulkDataPrefixGetsIndexWithNoConcepts(t *testing.T) {
	outDir := t.TempDir()

	// A prefix with a summary but zero promoted concepts (a parquet lake).
	summaries := []bundle.DirSummary{
		{Slug: "lake/events", ObjectCount: 10000, TotalSize: 4200000000},
	}

	if err := bundle.WriteBundle(nil, summaries, outDir); err != nil {
		t.Fatalf("WriteBundle returned error: %v", err)
	}

	content := readFile(t, filepath.Join(outDir, "lake", "events", "index.md"))
	if !strings.Contains(content, "- Object count: 10000") {
		t.Errorf("bulk-data index.md missing its summary, got:\n%s", content)
	}
	if strings.Contains(content, "## Concepts") {
		t.Errorf("bulk-data index.md should have no ## Concepts section, got:\n%s", content)
	}
	// The intermediate lake/ dir is implied and gets its own index linking events/.
	lakeIndex := readFile(t, filepath.Join(outDir, "lake", "index.md"))
	if !strings.Contains(lakeIndex, "events/index.md") {
		t.Errorf("lake/index.md missing link to events/, got:\n%s", lakeIndex)
	}
}

func TestWriteBundle_MultiLevelNestingGetsIndexAtEveryDir(t *testing.T) {
	outDir := t.TempDir()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	concepts := []bundle.Concept{
		{Slug: "orders/o.pdf", Type: "s3.object", Title: "o.pdf", Resource: "s3://b/orders/o.pdf", Timestamp: ts, Body: "x\n"},
		{Slug: "a/b/baz.pdf", Type: "s3.object", Title: "baz.pdf", Resource: "s3://b/a/b/baz.pdf", Timestamp: ts, Body: "x\n"},
	}

	if err := bundle.WriteBundle(concepts, nil, outDir); err != nil {
		t.Fatalf("WriteBundle returned error: %v", err)
	}

	for _, dir := range []string{".", "orders", "a", filepath.Join("a", "b")} {
		if _, err := os.Stat(filepath.Join(outDir, dir, "index.md")); err != nil {
			t.Errorf("expected index.md in %s: %v", dir, err)
		}
	}

	aIndex := readFile(t, filepath.Join(outDir, "a", "index.md"))
	if !strings.Contains(aIndex, "b/index.md") {
		t.Errorf("a/index.md missing link to child directory b/, got:\n%s", aIndex)
	}
	if strings.Contains(aIndex, "o.pdf") {
		t.Errorf("a/index.md should not link unrelated sibling o.pdf, got:\n%s", aIndex)
	}

	abIndex := readFile(t, filepath.Join(outDir, "a", "b", "index.md"))
	if !strings.Contains(abIndex, "baz.pdf.md") {
		t.Errorf("a/b/index.md missing link to child concept baz.pdf.md, got:\n%s", abIndex)
	}
}

func TestWriteBundle_LeadingSlashKeyDoesNotClobberRootIndex(t *testing.T) {
	outDir := t.TempDir()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	concepts := []bundle.Concept{
		{Slug: "/report.pdf", Type: "s3.object", Title: "report.pdf", Resource: "s3://b//report.pdf", Timestamp: ts, Body: "x\n"},
	}
	summaries := []bundle.DirSummary{{Slug: "", ObjectCount: 1, TotalSize: 100}}

	if err := bundle.WriteBundle(concepts, summaries, outDir); err != nil {
		t.Fatalf("WriteBundle returned error: %v", err)
	}

	rootIndex := readFile(t, filepath.Join(outDir, "index.md"))
	if !strings.Contains(rootIndex, "## Summary") || !strings.Contains(rootIndex, "- Object count: 1") {
		t.Errorf("root index.md lost its summary to an empty-named subdir, got:\n%s", rootIndex)
	}
	if !strings.Contains(rootIndex, "report.pdf.md") {
		t.Errorf("root index.md missing the concept filed at bundle root, got:\n%s", rootIndex)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return string(got)
}

func TestWriteBundle_RegeneratingIndexDropsStaleLinks(t *testing.T) {
	outDir := t.TempDir()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	first := []bundle.Concept{
		{Slug: "orders.pdf", Type: "s3.object", Title: "orders.pdf", Resource: "s3://b/orders.pdf", Timestamp: ts, Body: "x\n"},
		{Slug: "events.pdf", Type: "s3.object", Title: "events.pdf", Resource: "s3://b/events.pdf", Timestamp: ts, Body: "x\n"},
	}
	if err := bundle.WriteBundle(first, nil, outDir); err != nil {
		t.Fatalf("first WriteBundle: %v", err)
	}

	second := []bundle.Concept{
		{Slug: "orders.pdf", Type: "s3.object", Title: "orders.pdf", Resource: "s3://b/orders.pdf", Timestamp: ts, Body: "x\n"},
		{Slug: "users.pdf", Type: "s3.object", Title: "users.pdf", Resource: "s3://b/users.pdf", Timestamp: ts, Body: "x\n"},
	}
	if err := bundle.WriteBundle(second, nil, outDir); err != nil {
		t.Fatalf("second WriteBundle: %v", err)
	}

	index := readFile(t, filepath.Join(outDir, "index.md"))
	if strings.Contains(index, "events.pdf.md") {
		t.Errorf("index.md still links dropped concept events.pdf.md, got:\n%s", index)
	}
	if !strings.Contains(index, "users.pdf.md") {
		t.Errorf("index.md missing newly added concept users.pdf.md, got:\n%s", index)
	}
	if !strings.Contains(index, "orders.pdf.md") {
		t.Errorf("index.md missing unchanged concept orders.pdf.md, got:\n%s", index)
	}
}

func TestWriteBundle_OverwritesInPlace(t *testing.T) {
	outDir := t.TempDir()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	first := []bundle.Concept{{Slug: "orders.pdf", Type: "s3.object", Title: "orders.pdf", Resource: "s3://b/orders.pdf", Timestamp: ts, Body: "first\n"}}
	if err := bundle.WriteBundle(first, nil, outDir); err != nil {
		t.Fatalf("first WriteBundle: %v", err)
	}

	second := []bundle.Concept{{Slug: "orders.pdf", Type: "s3.object", Title: "orders.pdf", Resource: "s3://b/orders.pdf", Timestamp: ts, Body: "second\n"}}
	if err := bundle.WriteBundle(second, nil, outDir); err != nil {
		t.Fatalf("second WriteBundle: %v", err)
	}

	got := readFile(t, filepath.Join(outDir, "orders.pdf.md"))
	if strings.Contains(got, "first") {
		t.Errorf("expected overwrite, but stale content survived: %s", got)
	}
	if !strings.Contains(got, "second") {
		t.Errorf("expected new content, got: %s", got)
	}
}
