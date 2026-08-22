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
			Slug:      "orders",
			Type:      "s3.prefix",
			Title:     "orders/",
			Resource:  "s3://my-bucket/orders/",
			Timestamp: ts,
			Body:      "- Object count: 3\n- Total size: 300 bytes\n",
		},
		{
			Slug:      "events",
			Type:      "s3.prefix",
			Title:     "events/",
			Resource:  "s3://my-bucket/events/",
			Timestamp: ts,
			Body:      "- Object count: 1\n- Total size: 10 bytes\n",
		},
	}

	if err := bundle.WriteBundle(concepts, outDir); err != nil {
		t.Fatalf("WriteBundle returned error: %v", err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 files (2 concepts + index.md), got %d", len(entries))
	}

	for _, name := range []string{"orders.md", "events.md", "index.md"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Errorf("expected file %s to exist: %v", name, err)
		}
	}
}

func TestWriteBundle_FrontmatterAndBody(t *testing.T) {
	outDir := t.TempDir()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	concepts := []bundle.Concept{{
		Slug:      "orders",
		Type:      "s3.prefix",
		Title:     "orders/",
		Resource:  "s3://my-bucket/orders/",
		Timestamp: ts,
		Body:      "- Object count: 3\n",
	}}

	if err := bundle.WriteBundle(concepts, outDir); err != nil {
		t.Fatalf("WriteBundle returned error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outDir, "orders.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(got)

	if !strings.HasPrefix(content, "---\n") {
		t.Errorf("content does not start with frontmatter delimiter, got: %q", content)
	}
	for _, want := range []string{
		`type: "s3.prefix"`,
		`title: "orders/"`,
		`resource: "s3://my-bucket/orders/"`,
		`timestamp: "2026-01-01T12:00:00Z"`,
		`description: ""`,
		"- Object count: 3",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("content missing %q, got:\n%s", want, content)
		}
	}

	frontmatterEnd := strings.Index(content[4:], "---\n")
	if frontmatterEnd == -1 {
		t.Fatalf("no closing frontmatter delimiter found")
	}
}

func TestWriteBundle_NestedSlugCreatesSubdirectories(t *testing.T) {
	outDir := t.TempDir()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	concepts := []bundle.Concept{{
		Slug:      "a/b/baz",
		Type:      "s3.prefix",
		Title:     "a/b/baz/",
		Resource:  "s3://my-bucket/a/b/baz/",
		Timestamp: ts,
		Body:      "- Object count: 1\n",
	}}

	if err := bundle.WriteBundle(concepts, outDir); err != nil {
		t.Fatalf("WriteBundle returned error: %v", err)
	}

	wantPath := filepath.Join(outDir, "a", "b", "baz.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected nested file %s to exist: %v", wantPath, err)
	}
}

func TestWriteBundle_WritesRootIndexLinkingConcepts(t *testing.T) {
	outDir := t.TempDir()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	concepts := []bundle.Concept{
		{Slug: "orders", Type: "s3.prefix", Title: "orders/", Resource: "s3://b/orders/", Timestamp: ts, Body: "x\n"},
		{Slug: "events", Type: "s3.prefix", Title: "events/", Resource: "s3://b/events/", Timestamp: ts, Body: "x\n"},
	}

	if err := bundle.WriteBundle(concepts, outDir); err != nil {
		t.Fatalf("WriteBundle returned error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outDir, "index.md"))
	if err != nil {
		t.Fatalf("expected root index.md to exist: %v", err)
	}
	content := string(got)
	for _, want := range []string{"orders.md", "events.md"} {
		if !strings.Contains(content, want) {
			t.Errorf("root index.md missing link to %q, got:\n%s", want, content)
		}
	}
}

func TestWriteBundle_MultiLevelNestingGetsIndexAtEveryDir(t *testing.T) {
	outDir := t.TempDir()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	concepts := []bundle.Concept{
		{Slug: "orders", Type: "s3.prefix", Title: "orders/", Resource: "s3://b/orders/", Timestamp: ts, Body: "x\n"},
		{Slug: "a/b", Type: "s3.prefix", Title: "a/b/", Resource: "s3://b/a/b/", Timestamp: ts, Body: "x\n"},
		{Slug: "a/b/baz", Type: "s3.prefix", Title: "a/b/baz/", Resource: "s3://b/a/b/baz/", Timestamp: ts, Body: "x\n"},
	}

	if err := bundle.WriteBundle(concepts, outDir); err != nil {
		t.Fatalf("WriteBundle returned error: %v", err)
	}

	// index.md at every level: root, a/, a/b/
	for _, dir := range []string{".", "a", filepath.Join("a", "b")} {
		if _, err := os.Stat(filepath.Join(outDir, dir, "index.md")); err != nil {
			t.Errorf("expected index.md in %s: %v", dir, err)
		}
	}

	rootIndex := readFile(t, filepath.Join(outDir, "index.md"))
	if !strings.Contains(rootIndex, "orders.md") {
		t.Errorf("root index.md missing link to orders.md, got:\n%s", rootIndex)
	}
	if !strings.Contains(rootIndex, "a/index.md") {
		t.Errorf("root index.md missing link to child directory a/, got:\n%s", rootIndex)
	}

	aIndex := readFile(t, filepath.Join(outDir, "a", "index.md"))
	if !strings.Contains(aIndex, "b.md") {
		t.Errorf("a/index.md missing link to child concept b.md (the a/b prefix itself), got:\n%s", aIndex)
	}
	if !strings.Contains(aIndex, "b/index.md") {
		t.Errorf("a/index.md missing link to child directory b/ (holding a/b's children), got:\n%s", aIndex)
	}
	if strings.Contains(aIndex, "orders.md") {
		t.Errorf("a/index.md should not link unrelated sibling orders.md, got:\n%s", aIndex)
	}

	abIndex := readFile(t, filepath.Join(outDir, "a", "b", "index.md"))
	if !strings.Contains(abIndex, "baz.md") {
		t.Errorf("a/b/index.md missing link to child concept baz.md, got:\n%s", abIndex)
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
		{Slug: "orders", Type: "s3.prefix", Title: "orders/", Resource: "s3://b/orders/", Timestamp: ts, Body: "x\n"},
		{Slug: "events", Type: "s3.prefix", Title: "events/", Resource: "s3://b/events/", Timestamp: ts, Body: "x\n"},
	}
	if err := bundle.WriteBundle(first, outDir); err != nil {
		t.Fatalf("first WriteBundle: %v", err)
	}

	// events dropped, users added.
	second := []bundle.Concept{
		{Slug: "orders", Type: "s3.prefix", Title: "orders/", Resource: "s3://b/orders/", Timestamp: ts, Body: "x\n"},
		{Slug: "users", Type: "s3.prefix", Title: "users/", Resource: "s3://b/users/", Timestamp: ts, Body: "x\n"},
	}
	if err := bundle.WriteBundle(second, outDir); err != nil {
		t.Fatalf("second WriteBundle: %v", err)
	}

	index := readFile(t, filepath.Join(outDir, "index.md"))
	if strings.Contains(index, "events.md") {
		t.Errorf("index.md still links dropped concept events.md, got:\n%s", index)
	}
	if !strings.Contains(index, "users.md") {
		t.Errorf("index.md missing newly added concept users.md, got:\n%s", index)
	}
	if !strings.Contains(index, "orders.md") {
		t.Errorf("index.md missing unchanged concept orders.md, got:\n%s", index)
	}
}

func TestWriteBundle_OverwritesInPlace(t *testing.T) {
	outDir := t.TempDir()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	first := []bundle.Concept{{Slug: "orders", Type: "s3.prefix", Title: "orders/", Resource: "s3://b/orders/", Timestamp: ts, Body: "first\n"}}
	if err := bundle.WriteBundle(first, outDir); err != nil {
		t.Fatalf("first WriteBundle: %v", err)
	}

	second := []bundle.Concept{{Slug: "orders", Type: "s3.prefix", Title: "orders/", Resource: "s3://b/orders/", Timestamp: ts, Body: "second\n"}}
	if err := bundle.WriteBundle(second, outDir); err != nil {
		t.Fatalf("second WriteBundle: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outDir, "orders.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(got), "first") {
		t.Errorf("expected overwrite, but stale content survived: %s", got)
	}
	if !strings.Contains(string(got), "second") {
		t.Errorf("expected new content, got: %s", got)
	}
}
