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

// fakeS3Client implements s3walk.ListObjectsV2API. The walker only ever
// makes delimiter-based ("/") listings — one per prefix level — so the fake
// is keyed by the requested prefix and returns that level's child common
// prefixes (optionally paginated) alongside the objects sitting directly at
// it. A prefix with no scripted entry returns an empty level, which
// terminates recursion.
type fakeS3Client struct {
	// childPrefixes maps a parent prefix -> the pages of child common
	// prefixes returned for it. A single-element slice is one page.
	childPrefixes map[string][][]string
	// directObjects maps a parent prefix -> objects sitting directly at
	// that level (returned as Contents on the first page).
	directObjects map[string][]types.Object

	callsByPrefix map[string]int
}

func (f *fakeS3Client) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	prefix := aws.ToString(in.Prefix)

	if f.callsByPrefix == nil {
		f.callsByPrefix = map[string]int{}
	}
	pageIdx := f.callsByPrefix[prefix]
	f.callsByPrefix[prefix]++

	pages := f.childPrefixes[prefix]
	if pages == nil {
		pages = [][]string{nil}
	}
	if pageIdx >= len(pages) {
		return &s3.ListObjectsV2Output{IsTruncated: aws.Bool(false)}, nil
	}

	var common []types.CommonPrefix
	for _, p := range pages[pageIdx] {
		common = append(common, types.CommonPrefix{Prefix: aws.String(p)})
	}

	var contents []types.Object
	if pageIdx == 0 {
		contents = f.directObjects[prefix]
	}

	truncated := pageIdx < len(pages)-1
	out := &s3.ListObjectsV2Output{
		CommonPrefixes: common,
		Contents:       contents,
		IsTruncated:    aws.Bool(truncated),
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
		childPrefixes: map[string][][]string{"": {{"orders/"}}},
		directObjects: map[string][]types.Object{
			"orders/": {obj("orders/2026-01-01.csv", 100, t0)},
		},
	}

	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "", nil)
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
	if len(got.Objects) != 1 || got.Objects[0].Key != "orders/2026-01-01.csv" {
		t.Fatalf("Objects = %+v, want one entry keyed orders/2026-01-01.csv", got.Objects)
	}
	if got.Objects[0].Size != 100 || !got.Objects[0].LastModified.Equal(t0) {
		t.Errorf("Objects[0] = %+v, want size 100 modified %v", got.Objects[0], t0)
	}
}

func TestWalkPrefixes_DirectObjectsAllListedUncapped(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	client := &fakeS3Client{
		childPrefixes: map[string][][]string{"": {{"events/"}}},
		directObjects: map[string][]types.Object{
			"events/": {
				obj("events/a.json", 10, t0),
				obj("events/b.json", 20, t1),
				obj("events/c.json", 30, t2),
				obj("events/d.json", 40, t0),
				obj("events/e.json", 50, t0),
				obj("events/f.json", 60, t0),
			},
		},
	}

	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "", nil)
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
	if len(got.Objects) != 6 {
		t.Errorf("Objects len = %d, want all 6 (no cap)", len(got.Objects))
	}
}

func TestWalkPrefixes_PaginatesChildPrefixDiscovery(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	client := &fakeS3Client{
		childPrefixes: map[string][][]string{
			"": {{"orders/"}, {"events/"}},
		},
		directObjects: map[string][]types.Object{
			"orders/": {obj("orders/1.csv", 1, t0)},
			"events/": {obj("events/1.json", 2, t0)},
		},
	}

	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "", nil)
	if err != nil {
		t.Fatalf("WalkPrefixes returned error: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries across two discovery pages, got %d", len(summaries))
	}
}

func TestWalkPrefixes_RecursesIntoNestedPrefixes(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	client := &fakeS3Client{
		childPrefixes: map[string][][]string{
			"":     {{"a/"}},
			"a/":   {{"a/b/"}},
			"a/b/": {{"a/b/c/"}},
		},
		directObjects: map[string][]types.Object{
			"a/":     {obj("a/1.txt", 1, t0)},
			"a/b/":   {obj("a/b/1.txt", 2, t0)},
			"a/b/c/": {obj("a/b/c/1.txt", 3, t0)},
		},
	}

	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "", nil)
	if err != nil {
		t.Fatalf("WalkPrefixes returned error: %v", err)
	}

	var gotPrefixes []string
	for _, s := range summaries {
		gotPrefixes = append(gotPrefixes, s.Prefix)
	}
	want := []string{"a/", "a/b/", "a/b/c/"}
	if len(gotPrefixes) != len(want) {
		t.Fatalf("got prefixes %v, want %v", gotPrefixes, want)
	}
	for i, w := range want {
		if gotPrefixes[i] != w {
			t.Errorf("summaries[%d].Prefix = %q, want %q (order %v)", i, gotPrefixes[i], w, gotPrefixes)
		}
	}
}

func TestWalkPrefixes_EachLevelCountsOnlyItsDirectObjects(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	client := &fakeS3Client{
		childPrefixes: map[string][][]string{
			"a/": {{"a/b/"}},
		},
		directObjects: map[string][]types.Object{
			"a/":   {obj("a/root-doc.pdf", 5, t0)},
			"a/b/": {obj("a/b/1.parquet", 100, t0), obj("a/b/2.parquet", 200, t0)},
		},
	}

	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "a/", nil)
	if err != nil {
		t.Fatalf("WalkPrefixes returned error: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d: %+v", len(summaries), summaries)
	}
	if summaries[0].Prefix != "a/" || summaries[0].ObjectCount != 1 || summaries[0].TotalSize != 5 {
		t.Errorf("summaries[0] = %+v, want a/ with its 1 direct object only, not the subtree", summaries[0])
	}
	if summaries[1].Prefix != "a/b/" || summaries[1].ObjectCount != 2 || summaries[1].TotalSize != 300 {
		t.Errorf("summaries[1] = %+v, want a/b/ with 2 objects / 300 bytes", summaries[1])
	}
}

func TestWalkPrefixes_PrefixWithOnlySubPrefixesYieldsNoSummaryForItself(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	client := &fakeS3Client{
		childPrefixes: map[string][][]string{
			"": {{"orders/", "events/"}},
		},
		directObjects: map[string][]types.Object{
			"orders/": {obj("orders/1.csv", 1, t0)},
			"events/": {obj("events/1.json", 2, t0)},
		},
	}

	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "", nil)
	if err != nil {
		t.Fatalf("WalkPrefixes returned error: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, no rolled-up root summary, got %d: %+v", len(summaries), summaries)
	}
}

func TestWalkPrefixes_FlatObjectsUnderRootWithNoSubPrefixes(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	client := &fakeS3Client{
		directObjects: map[string][]types.Object{
			"some/path/": {obj("some/path/report.pdf", 100, t0)},
		},
	}

	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "some/path/", nil)
	if err != nil {
		t.Fatalf("WalkPrefixes returned error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary for the flat root prefix, got %d", len(summaries))
	}

	got := summaries[0]
	if got.Prefix != "some/path/" {
		t.Errorf("Prefix = %q, want %q", got.Prefix, "some/path/")
	}
	if got.ObjectCount != 1 || got.TotalSize != 100 {
		t.Errorf("summary = %+v, want 1 object / 100 bytes", got)
	}
	if len(got.Objects) != 1 || got.Objects[0].Key != "some/path/report.pdf" {
		t.Errorf("Objects = %+v, want [some/path/report.pdf]", got.Objects)
	}
}

func TestWalkPrefixes_RootWithBothDirectObjectsAndSubPrefixes(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	client := &fakeS3Client{
		childPrefixes: map[string][][]string{
			"some/path/": {{"some/path/orders/"}},
		},
		directObjects: map[string][]types.Object{
			"some/path/":        {obj("some/path/report.pdf", 100, t0)},
			"some/path/orders/": {obj("some/path/orders/1.csv", 1, t0)},
		},
	}

	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "some/path/", nil)
	if err != nil {
		t.Fatalf("WalkPrefixes returned error: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries (root + sub-prefix), got %d", len(summaries))
	}
	if summaries[0].Prefix != "some/path/" || summaries[0].ObjectCount != 1 {
		t.Errorf("summaries[0] = %+v, want root %q with 1 direct object", summaries[0], "some/path/")
	}
	if summaries[1].Prefix != "some/path/orders/" {
		t.Errorf("summaries[1].Prefix = %q, want %q", summaries[1].Prefix, "some/path/orders/")
	}
}

func TestWalkPrefixes_Paginates(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	client := &fakeS3Client{
		childPrefixes: map[string][][]string{
			"":      {{"logs/"}},
			"logs/": {{}, {}},
		},
		directObjects: map[string][]types.Object{
			"logs/": {obj("logs/1.txt", 1, t0)},
		},
	}

	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "", nil)
	if err != nil {
		t.Fatalf("WalkPrefixes returned error: %v", err)
	}
	got := summaries[0]
	if got.ObjectCount != 1 || got.TotalSize != 1 {
		t.Errorf("summary = %+v, want 1 object / 1 byte across paginated discovery", got)
	}
}

func TestWalkPrefixes_KeepPredicateBoundsObjectsButNotAggregate(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	client := &fakeS3Client{
		directObjects: map[string][]types.Object{
			"docs/": {
				obj("docs/report.pdf", 100, t0),
				obj("docs/part-0001.parquet", 900, t0),
				obj("docs/part-0002.parquet", 900, t0),
			},
		},
	}

	keepPDF := func(key string) bool { return len(key) > 4 && key[len(key)-4:] == ".pdf" }
	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "docs/", keepPDF)
	if err != nil {
		t.Fatalf("WalkPrefixes returned error: %v", err)
	}

	got := summaries[0]
	if got.ObjectCount != 3 || got.TotalSize != 1900 {
		t.Errorf("aggregate = %d objs / %d bytes, want 3 / 1900 (all direct objects)", got.ObjectCount, got.TotalSize)
	}
	if len(got.Objects) != 1 || got.Objects[0].Key != "docs/report.pdf" {
		t.Errorf("Objects = %+v, want only the kept .pdf", got.Objects)
	}
}

func TestWalkPrefixes_EmptyRootYieldsNoSummary(t *testing.T) {
	client := &fakeS3Client{}

	summaries, err := s3walk.WalkPrefixes(context.Background(), client, "my-bucket", "empty/", nil)
	if err != nil {
		t.Fatalf("WalkPrefixes returned error: %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries for an empty prefix, got %d", len(summaries))
	}
}
