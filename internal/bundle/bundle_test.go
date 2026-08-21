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
	if len(entries) != 2 {
		t.Fatalf("expected 2 files, got %d", len(entries))
	}

	for _, name := range []string{"orders.md", "events.md"} {
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
