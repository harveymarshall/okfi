package s3walk_test

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/harveymarshall/okfi/internal/s3walk"
)

// fakeS3Client implements s3walk.ListObjectsV2API with canned responses,
// keyed by delimiter so the walker's two call shapes (prefix discovery vs.
// per-prefix object listing) can be scripted independently in one fake.
type fakeS3Client struct {
	// commonPrefixes is returned for the delimiter-scoped discovery call,
	// as a single page. For multi-page discovery, set commonPrefixPages
	// instead.
	commonPrefixes []string
	// commonPrefixPages, if set, overrides commonPrefixes: each entry is
	// one page of the delimiter-scoped discovery call.
	commonPrefixPages [][]string
	// objectsByPrefix is returned for the per-prefix, no-delimiter call.
	// Each entry is one "page"; a fake reads through them in order per prefix.
	objectsByPrefix map[string][][]types.Object
	callsByPrefix   map[string]int
	discoveryCalls  int
}

func (f *fakeS3Client) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if in.Delimiter != nil && *in.Delimiter == "/" {
		pages := f.commonPrefixPages
		if pages == nil {
			pages = [][]string{f.commonPrefixes}
		}

		pageIdx := f.discoveryCalls
		f.discoveryCalls++
		if pageIdx >= len(pages) {
			return &s3.ListObjectsV2Output{IsTruncated: aws.Bool(false)}, nil
		}

		var prefixes []types.CommonPrefix
		for _, p := range pages[pageIdx] {
			prefixes = append(prefixes, types.CommonPrefix{Prefix: aws.String(p)})
		}

		truncated := pageIdx < len(pages)-1
		out := &s3.ListObjectsV2Output{CommonPrefixes: prefixes, IsTruncated: aws.Bool(truncated)}
		if truncated {
			out.NextContinuationToken = aws.String("discovery-token-" + string(rune('0'+pageIdx+1)))
		}
		return out, nil
	}

	prefix := aws.ToString(in.Prefix)
	if f.callsByPrefix == nil {
		f.callsByPrefix = map[string]int{}
	}
	pageIdx := f.callsByPrefix[prefix]
	f.callsByPrefix[prefix]++

	pages := f.objectsByPrefix[prefix]
	if pageIdx >= len(pages) {
		return &s3.ListObjectsV2Output{IsTruncated: aws.Bool(false)}, nil
	}

	truncated := pageIdx < len(pages)-1
	out := &s3.ListObjectsV2Output{
		Contents:    pages[pageIdx],
		IsTruncated: aws.Bool(truncated),
	}
	if truncated {
		out.NextContinuationToken = aws.String(prefix + "-token-" + string(rune('0'+pageIdx+1)))
	}
	return out, nil
}

func obj(key string, size int64, modified time.Time) types.Object {
	return types.Object{Key: aws.String(key), Size: aws.Int64(size), LastModified: aws.Time(modified)}
}

func TestWalkPrefixes_SinglePrefixSingleObject(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	client := &fakeS3Client{
		commonPrefixes: []string{"orders/"},
		objectsByPrefix: map[string][][]types.Object{
			"orders/": {{obj("orders/2026-01-01.csv", 100, t0)}},
		},
	}

	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "")
	if err != nil {
		t.Fatalf("WalkPrefixes returned error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}

	got := summaries[0]
	if got.Prefix != "orders/" {
		t.Errorf("Prefix = %q, want %q", got.Prefix, "orders/")
	}
	if got.ObjectCount != 1 {
		t.Errorf("ObjectCount = %d, want 1", got.ObjectCount)
	}
	if got.TotalSize != 100 {
		t.Errorf("TotalSize = %d, want 100", got.TotalSize)
	}
	if !got.MinModified.Equal(t0) || !got.MaxModified.Equal(t0) {
		t.Errorf("MinModified/MaxModified = %v/%v, want both %v", got.MinModified, got.MaxModified, t0)
	}
	if len(got.SampleKeys) != 1 || got.SampleKeys[0] != "orders/2026-01-01.csv" {
		t.Errorf("SampleKeys = %v, want [orders/2026-01-01.csv]", got.SampleKeys)
	}
}

func TestWalkPrefixes_AggregatesAndCapsSamples(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	client := &fakeS3Client{
		commonPrefixes: []string{"events/"},
		objectsByPrefix: map[string][][]types.Object{
			"events/": {{
				obj("events/a.json", 10, t0),
				obj("events/b.json", 20, t1),
				obj("events/c.json", 30, t2),
				obj("events/d.json", 40, t0),
				obj("events/e.json", 50, t0),
				obj("events/f.json", 60, t0),
			}},
		},
	}

	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "")
	if err != nil {
		t.Fatalf("WalkPrefixes returned error: %v", err)
	}
	got := summaries[0]

	if got.ObjectCount != 6 {
		t.Errorf("ObjectCount = %d, want 6", got.ObjectCount)
	}
	if got.TotalSize != 210 {
		t.Errorf("TotalSize = %d, want 210", got.TotalSize)
	}
	if !got.MinModified.Equal(t0) {
		t.Errorf("MinModified = %v, want %v", got.MinModified, t0)
	}
	if !got.MaxModified.Equal(t1) {
		t.Errorf("MaxModified = %v, want %v", got.MaxModified, t1)
	}
	if len(got.SampleKeys) != s3walk.MaxSampleKeys {
		t.Errorf("SampleKeys len = %d, want capped at %d", len(got.SampleKeys), s3walk.MaxSampleKeys)
	}
}

func TestWalkPrefixes_Paginates(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	client := &fakeS3Client{
		commonPrefixes: []string{"logs/"},
		objectsByPrefix: map[string][][]types.Object{
			"logs/": {
				{obj("logs/1.txt", 1, t0)},
				{obj("logs/2.txt", 2, t0)},
			},
		},
	}

	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "")
	if err != nil {
		t.Fatalf("WalkPrefixes returned error: %v", err)
	}
	got := summaries[0]

	if got.ObjectCount != 2 {
		t.Errorf("ObjectCount = %d, want 2 (across two pages)", got.ObjectCount)
	}
	if got.TotalSize != 3 {
		t.Errorf("TotalSize = %d, want 3", got.TotalSize)
	}
}

func TestWalkPrefixes_PaginatesTopLevelPrefixDiscovery(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	client := &fakeS3Client{
		commonPrefixPages: [][]string{
			{"orders/"},
			{"events/"},
		},
		objectsByPrefix: map[string][][]types.Object{
			"orders/": {{obj("orders/1.csv", 1, t0)}},
			"events/": {{obj("events/1.json", 2, t0)}},
		},
	}

	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "")
	if err != nil {
		t.Fatalf("WalkPrefixes returned error: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries across two discovery pages, got %d", len(summaries))
	}
}

func TestWalkPrefixes_MultipleTopLevelPrefixes(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	client := &fakeS3Client{
		commonPrefixes: []string{"orders/", "events/"},
		objectsByPrefix: map[string][][]types.Object{
			"orders/": {{obj("orders/1.csv", 1, t0)}},
			"events/": {{obj("events/1.json", 2, t0)}},
		},
	}

	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "")
	if err != nil {
		t.Fatalf("WalkPrefixes returned error: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
}
