// Package s3walk recursively discovers prefixes in an S3 bucket, at every
// nesting level, and summarizes each one (object count, total size, mtime
// range, sample keys) for shallow OKF bundle generation. It does not
// inspect object contents.
package s3walk

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// MaxSampleKeys caps how many object keys are recorded per prefix summary.
const MaxSampleKeys = 5

// ListObjectsV2API is the minimal S3 surface WalkPrefixes needs. The
// concrete *s3.Client satisfies it, and tests supply a fake.
type ListObjectsV2API interface {
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

// PrefixSummary is the shallow inventory of one top-level prefix.
type PrefixSummary struct {
	Prefix      string
	ObjectCount int
	TotalSize   int64
	MinModified time.Time
	MaxModified time.Time
	SampleKeys  []string
}

// WalkPrefixes recursively discovers every prefix nested under rootPrefix in
// bucket, at every depth, and returns a shallow summary of each: object
// count, total size, mtime range, and a capped sample of object keys. It
// does not read object contents. Results are depth-first, no recursion cap.
//
// rootPrefix itself is also summarized (its own summary comes first) when
// it has objects sitting directly under it — not nested under any further
// sub-prefix. Without this, a prefix with no sub-prefixes at all (e.g. a
// single flat object) would discover zero common prefixes and yield zero
// summaries despite having content. The root summary counts only its own
// direct objects, not the full recursive subtree — descendants already get
// their own summaries, so aggregating them again here would double-count.
func WalkPrefixes(ctx context.Context, client ListObjectsV2API, bucket, rootPrefix string) ([]PrefixSummary, error) {
	level, err := listPrefixLevel(ctx, client, bucket, rootPrefix)
	if err != nil {
		return nil, err
	}

	var summaries []PrefixSummary
	if len(level.direct) > 0 {
		summary := PrefixSummary{Prefix: rootPrefix}
		for _, obj := range level.direct {
			accumulate(&summary, obj)
		}
		summaries = append(summaries, summary)
	}

	children, err := walkChildren(ctx, client, bucket, level.children)
	if err != nil {
		return nil, err
	}
	summaries = append(summaries, children...)
	return summaries, nil
}

// walkChildren summarizes each prefix in children (each one's full
// recursive subtree, via summarizePrefix) and recurses into its own
// nested sub-prefixes, at every depth.
func walkChildren(ctx context.Context, client ListObjectsV2API, bucket string, children []string) ([]PrefixSummary, error) {
	var summaries []PrefixSummary
	for _, prefix := range children {
		summary, err := summarizePrefix(ctx, client, bucket, prefix)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)

		level, err := listPrefixLevel(ctx, client, bucket, prefix)
		if err != nil {
			return nil, err
		}
		grandchildren, err := walkChildren(ctx, client, bucket, level.children)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, grandchildren...)
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

func summarizePrefix(ctx context.Context, client ListObjectsV2API, bucket, prefix string) (PrefixSummary, error) {
	summary := PrefixSummary{Prefix: prefix}

	var continuationToken *string
	for {
		out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return PrefixSummary{}, err
		}

		for _, obj := range out.Contents {
			accumulate(&summary, obj)
		}

		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		continuationToken = out.NextContinuationToken
	}

	return summary, nil
}

func accumulate(summary *PrefixSummary, obj types.Object) {
	summary.ObjectCount++
	summary.TotalSize += aws.ToInt64(obj.Size)

	if modified := aws.ToTime(obj.LastModified); !modified.IsZero() {
		if summary.MinModified.IsZero() || modified.Before(summary.MinModified) {
			summary.MinModified = modified
		}
		if modified.After(summary.MaxModified) {
			summary.MaxModified = modified
		}
	}

	if len(summary.SampleKeys) < MaxSampleKeys {
		summary.SampleKeys = append(summary.SampleKeys, aws.ToString(obj.Key))
	}
}
