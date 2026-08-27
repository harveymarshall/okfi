// Package s3walk recursively discovers prefixes in an S3 bucket, at every
// nesting level, and summarizes each one (object count, total size, mtime
// range) plus the objects sitting directly at that level, for shallow OKF
// bundle generation. It does not inspect object contents.
package s3walk

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ListObjectsV2API is the minimal S3 surface WalkPrefixes needs. The
// concrete *s3.Client satisfies it, and tests supply a fake.
type ListObjectsV2API interface {
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

// ObjectInfo is the shallow record of one S3 object sitting directly at a
// prefix level: enough for a caller to decide whether to promote it to a
// concept and to describe it without reading its contents.
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// PrefixSummary is the shallow, direct-level inventory of one prefix: the
// aggregate over the objects sitting directly under it (not a recursive
// subtree rollup — each nested prefix gets its own summary), plus the list
// of those direct objects the caller asked to keep (see WalkPrefixes'
// keep predicate).
type PrefixSummary struct {
	Prefix      string
	ObjectCount int
	TotalSize   int64
	MinModified time.Time
	MaxModified time.Time
	Objects     []ObjectInfo
}

// KeepFunc reports whether an object's detail should be retained in a
// PrefixSummary's Objects list. It does not affect the aggregate (count,
// size, mtime range), which always covers every direct object.
type KeepFunc func(key string) bool

// WalkPrefixes recursively discovers every prefix nested under rootPrefix in
// bucket, at every depth, and returns a shallow summary of each level: the
// aggregate (object count, total size, mtime range) and the list of objects
// sitting directly at that level. It does not read object contents.
//
// Each summary counts only its level's direct objects — never the recursive
// subtree — because every descendant prefix already gets its own summary,
// so rolling their objects up here would double-count on descent. A prefix
// with no direct objects at all (only sub-prefixes) yields no summary of its
// own; its sub-prefixes still do. Results are depth-first, no recursion cap.
//
// keep bounds how much per-object detail is held in memory: only objects
// for which keep returns true are recorded in each PrefixSummary.Objects.
// A nil keep retains every object. The aggregate always covers all direct
// objects regardless.
func WalkPrefixes(ctx context.Context, client ListObjectsV2API, bucket, rootPrefix string, keep KeepFunc) ([]PrefixSummary, error) {
	return walkLevel(ctx, client, bucket, rootPrefix, keep)
}

// walkLevel summarizes prefix from its direct objects (if any) and recurses
// into each of its immediate child prefixes, at every depth.
func walkLevel(ctx context.Context, client ListObjectsV2API, bucket, prefix string, keep KeepFunc) ([]PrefixSummary, error) {
	level, err := listPrefixLevel(ctx, client, bucket, prefix)
	if err != nil {
		return nil, err
	}

	var summaries []PrefixSummary
	if len(level.direct) > 0 {
		summary := PrefixSummary{Prefix: prefix}
		for _, obj := range level.direct {
			accumulate(&summary, obj, keep)
		}
		summaries = append(summaries, summary)
	}

	for _, child := range level.children {
		childSummaries, err := walkLevel(ctx, client, bucket, child, keep)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, childSummaries...)
	}

	return summaries, nil
}

// prefixLevel is the result of one delimiter-based listing at a single
// prefix level: its immediate child common prefixes, and any objects
// sitting directly at that level (not nested under a further
// "/"-delimited child).
type prefixLevel struct {
	children []string
	direct   []types.Object
}

func listPrefixLevel(ctx context.Context, client ListObjectsV2API, bucket, prefix string) (prefixLevel, error) {
	var level prefixLevel
	var continuationToken *string

	for {
		out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String(prefix),
			Delimiter:         aws.String("/"),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return prefixLevel{}, err
		}

		for _, cp := range out.CommonPrefixes {
			level.children = append(level.children, aws.ToString(cp.Prefix))
		}
		level.direct = append(level.direct, out.Contents...)

		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		continuationToken = out.NextContinuationToken
	}

	return level, nil
}

func accumulate(summary *PrefixSummary, obj types.Object, keep KeepFunc) {
	summary.ObjectCount++
	summary.TotalSize += aws.ToInt64(obj.Size)

	modified := aws.ToTime(obj.LastModified)
	if !modified.IsZero() {
		if summary.MinModified.IsZero() || modified.Before(summary.MinModified) {
			summary.MinModified = modified
		}
		if modified.After(summary.MaxModified) {
			summary.MaxModified = modified
		}
	}

	key := aws.ToString(obj.Key)
	if keep == nil || keep(key) {
		summary.Objects = append(summary.Objects, ObjectInfo{
			Key:          key,
			Size:         aws.ToInt64(obj.Size),
			LastModified: modified,
		})
	}
}
