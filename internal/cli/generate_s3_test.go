package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/harveymarshall/okfi/internal/s3walk"
)

func TestToObjectConcept_SlugKeepsFullKeyForNestedOutputPath(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		wantSlug string
		wantTit  string
	}{
		{name: "root object", key: "bigshop-flow-diagram.pdf", wantSlug: "bigshop-flow-diagram.pdf", wantTit: "bigshop-flow-diagram.pdf"},
		{name: "nested object", key: "reports/2026/q1.pdf", wantSlug: "reports/2026/q1.pdf", wantTit: "q1.pdf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := toObjectConcept("my-bucket", s3walk.ObjectInfo{Key: tt.key, Size: 10}, time.Now())
			if c.Slug != tt.wantSlug {
				t.Errorf("Slug = %q, want %q", c.Slug, tt.wantSlug)
			}
			if c.Title != tt.wantTit {
				t.Errorf("Title = %q, want %q", c.Title, tt.wantTit)
			}
			if c.Type != "s3.object" {
				t.Errorf("Type = %q, want s3.object", c.Type)
			}
			if want := "s3://my-bucket/" + tt.key; c.Resource != want {
				t.Errorf("Resource = %q, want %q", c.Resource, want)
			}
		})
	}
}

func TestToObjectConcept_BodyHasKeySizeModified(t *testing.T) {
	mod := time.Date(2026, 8, 22, 14, 39, 12, 0, time.UTC)
	c := toObjectConcept("b", s3walk.ObjectInfo{Key: "a/report.pdf", Size: 198150, LastModified: mod}, time.Now())

	for _, want := range []string{
		"- Key: a/report.pdf",
		"- Size: 198150 bytes",
		"- Last modified: 2026-08-22T14:39:12Z",
	} {
		if !strings.Contains(c.Body, want) {
			t.Errorf("Body missing %q, got:\n%s", want, c.Body)
		}
	}
}

func TestToDirSummary_TrimsSlashesFromSlug(t *testing.T) {
	tests := []struct {
		prefix   string
		wantSlug string
	}{
		{prefix: "", wantSlug: ""},
		{prefix: "orders/", wantSlug: "orders"},
		{prefix: "a/b/baz/", wantSlug: "a/b/baz"},
	}
	for _, tt := range tests {
		got := toDirSummary(s3walk.PrefixSummary{Prefix: tt.prefix, ObjectCount: 2, TotalSize: 5})
		if got.Slug != tt.wantSlug {
			t.Errorf("prefix %q: Slug = %q, want %q", tt.prefix, got.Slug, tt.wantSlug)
		}
		if got.ObjectCount != 2 || got.TotalSize != 5 {
			t.Errorf("prefix %q: aggregate not carried through: %+v", tt.prefix, got)
		}
	}
}

func TestNewExtensionFilter(t *testing.T) {
	tests := []struct {
		name  string
		types []string
		key   string
		want  bool
	}{
		{name: "allowed extension", types: []string{"pdf", "md"}, key: "docs/report.pdf", want: true},
		{name: "denied extension", types: []string{"pdf", "md"}, key: "data/part-0001.parquet", want: false},
		{name: "case-insensitive key", types: []string{"pdf"}, key: "docs/REPORT.PDF", want: true},
		{name: "case-insensitive allowlist", types: []string{"PDF"}, key: "docs/report.pdf", want: true},
		{name: "leading dot in allowlist", types: []string{".pdf"}, key: "docs/report.pdf", want: true},
		{name: "star promotes everything", types: []string{"*"}, key: "data/part-0001.parquet", want: true},
		{name: "no extension", types: []string{"pdf"}, key: "docs/README", want: false},
		{name: "directory marker never promoted", types: []string{"*"}, key: "docs/", want: false},
		{name: "blank allowlist entry ignored", types: []string{"pdf", ""}, key: "docs/README", want: false},
		{name: "blank entry does not gate star", types: []string{""}, key: "a.pdf", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newExtensionFilter(tt.types)(tt.key); got != tt.want {
				t.Errorf("filter(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestParseS3URI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantBucket string
		wantPrefix string
		wantErr    bool
	}{
		{
			name:       "bucket and prefix, no trailing slash",
			uri:        "s3://my-bucket/some/path",
			wantBucket: "my-bucket",
			wantPrefix: "some/path/",
		},
		{
			name:       "bucket and prefix, trailing slash already present",
			uri:        "s3://my-bucket/some/path/",
			wantBucket: "my-bucket",
			wantPrefix: "some/path/",
		},
		{
			name:       "bare bucket, no prefix",
			uri:        "s3://my-bucket",
			wantBucket: "my-bucket",
			wantPrefix: "",
		},
		{
			name:    "missing scheme",
			uri:     "my-bucket/some/path",
			wantErr: true,
		},
		{
			name:    "missing bucket",
			uri:     "s3://",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, prefix, err := parseS3URI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got bucket=%q prefix=%q", bucket, prefix)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if bucket != tt.wantBucket {
				t.Errorf("bucket = %q, want %q", bucket, tt.wantBucket)
			}
			if prefix != tt.wantPrefix {
				t.Errorf("prefix = %q, want %q", prefix, tt.wantPrefix)
			}
		})
	}
}
