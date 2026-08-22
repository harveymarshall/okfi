# okfi

Golang CLI tool to create and manage [Google OKF](https://cloud.google.com/blog/products/data-analytics/how-the-open-knowledge-format-can-improve-data-sharing)
(Open Knowledge Format) bundles from data resources — BigQuery, Redshift,
S3, GCS.

An OKF bundle is a directory of markdown files with YAML frontmatter, one
concept per file, giving AI agents grounded, portable context about your
data estate.

## Status

v0.x, pre-1.0. Only S3 is implemented so far — shallow inspection (bucket
prefix structure, object counts/sizes/mtimes), no file-content schema
inference. BigQuery, Redshift, and GCS are not yet implemented.

## Install

```sh
go install github.com/harveymarshall/okfi/cmd/okfi@latest
```

## Usage

```sh
okfi generate s3 s3://my-bucket/some/path --out ./bundle
```

Generates one markdown file per prefix nested under
`s3://my-bucket/some/path`, at every level, into `./bundle` — output paths
mirror the source prefix hierarchy (e.g. prefix `a/b/baz/` writes to
`bundle/a/b/baz.md`). Each file has OKF frontmatter
(`type: s3.prefix`, `title`, `resource`, `timestamp`) and a body listing
object count, total size, last-modified range, and a sample of object keys.

Flags:

- `--out` (required) — output directory for the generated bundle.
- `--region` — AWS region, optional.
- `--profile` — AWS shared-config profile, optional.

Authentication uses the [AWS SDK default credential chain](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-gosdk.html#credentials)
(environment variables, shared credentials/config file, IAM role) — no
credential flags.

Re-running against the same `--out` directory overwrites each concept's
file in place; there's no diff/merge, and prefixes removed from the bucket
leave stale files behind (pruning is not yet implemented).

## Development

```sh
go build ./...
go vet ./...
go test ./...
```
