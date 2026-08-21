// Package s3walk discovers top-level prefixes in an S3 bucket and
// summarizes each one (object count, total size, mtime range, sample keys)
// for shallow OKF bundle generation. It does not inspect object contents.
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

// WalkPrefixes lists the top-level prefixes under rootPrefix in bucket and
// returns a shallow summary of each: object count, total size, mtime range,
// and a capped sample of object keys. It does not read object contents.
func WalkPrefixes(ctx context.Context, client ListObjectsV2API, bucket, rootPrefix string) ([]PrefixSummary, error) {
	prefixes, err := listCommonPrefixes(ctx, client, bucket, rootPrefix)
	if err != nil {
		return nil, err
	}

	summaries := make([]PrefixSummary, 0, len(prefixes))
	for _, prefix := range prefixes {
		summary, err := summarizePrefix(ctx, client, bucket, prefix)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func listCommonPrefixes(ctx context.Context, client ListObjectsV2API, bucket, rootPrefix string) ([]string, error) {
	out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String(rootPrefix),
		Delimiter: aws.String("/"),
	})
	if err != nil {
		return nil, err
	}

	prefixes := make([]string, 0, len(out.CommonPrefixes))
	for _, cp := range out.CommonPrefixes {
		prefixes = append(prefixes, aws.ToString(cp.Prefix))
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
