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
// rootPrefix itself is also summarized (its own summary comes first), so
// objects sitting directly under it are not lost — otherwise a prefix with
// no further nested sub-prefixes (e.g. a single flat object) would discover
// zero common prefixes and yield zero summaries despite having content. The
// root summary is omitted when rootPrefix has no objects at all.
func WalkPrefixes(ctx context.Context, client ListObjectsV2API, bucket, rootPrefix string) ([]PrefixSummary, error) {
	root, err := summarizePrefix(ctx, client, bucket, rootPrefix)
	if err != nil {
		return nil, err
	}

	var summaries []PrefixSummary
	if root.ObjectCount > 0 {
		summaries = append(summaries, root)
	}

	children, err := walkChildren(ctx, client, bucket, rootPrefix)
	if err != nil {
		return nil, err
	}
	summaries = append(summaries, children...)
	return summaries, nil
}

// walkChildren recursively discovers and summarizes every prefix nested
// under parentPrefix, at every depth. Unlike WalkPrefixes, it does not
// summarize parentPrefix itself — only its descendants.
func walkChildren(ctx context.Context, client ListObjectsV2API, bucket, parentPrefix string) ([]PrefixSummary, error) {
	prefixes, err := listCommonPrefixes(ctx, client, bucket, parentPrefix)
	if err != nil {
		return nil, err
	}

	var summaries []PrefixSummary
	for _, prefix := range prefixes {
		summary, err := summarizePrefix(ctx, client, bucket, prefix)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)

		children, err := walkChildren(ctx, client, bucket, prefix)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, children...)
	}
	return summaries, nil
}

func listCommonPrefixes(ctx context.Context, client ListObjectsV2API, bucket, rootPrefix string) ([]string, error) {
	var prefixes []string
	var continuationToken *string

	for {
		out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String(rootPrefix),
			Delimiter:         aws.String("/"),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, err
		}

		for _, cp := range out.CommonPrefixes {
			prefixes = append(prefixes, aws.ToString(cp.Prefix))
		}

		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		continuationToken = out.NextContinuationToken
	}

	return prefixes, nil
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
