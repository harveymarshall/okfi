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

Concepts are individual S3 objects, filtered by extension (see
`--object-types`) — each becomes an annotatable markdown file pointing an
agent at the object to read. S3 prefixes are not concepts; each directory
level gets an `index.md` summarising how much data sits directly there.

## Install

```sh
go install github.com/harveymarshall/okfi/cmd/okfi@latest
```

## Usage

```sh
okfi generate s3 s3://my-bucket/some/path --out ./bundle
```

Walks every prefix level under `s3://my-bucket/some/path` and writes, into
`./bundle`:

- **One concept per promoted object.** An object whose extension is in the
  allowlist (`--object-types`) becomes a markdown file at a path mirroring
  its key (`some/path/report.pdf` → `bundle/some/path/report.pdf.md`), with
  OKF frontmatter (`type: s3.object`, `title`, `resource` — the full
  `s3://` URI to read — `timestamp`, and a blank `description` for a
  human/LLM to fill in) and a body listing the object's key, size, and
  last-modified time.
- **An `index.md` at every directory level.** Each carries a `## Summary`
  (object count, total size, last-modified range for the objects sitting
  directly at that level) followed by links to its child concepts and
  subdirectories.

Non-promoted objects (bulk / partitioned data) are not given concept
files; they are still counted in their level's `index.md` summary.

Flags:

- `--out` (required) — output directory for the generated bundle.
- `--object-types` — comma-separated file extensions to promote to
  concepts. Default: `pdf,md,markdown,txt,rst,docx,doc,xlsx,pptx`. Pass
  `--object-types '*'` to promote every object; matching is
  case-insensitive on the key's final extension.
- `--region` — AWS region, optional.
- `--profile` — AWS shared-config profile, optional.

Authentication uses the [AWS SDK default credential chain](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-gosdk.html#credentials)
(environment variables, shared credentials/config file, IAM role) — no
credential flags.

Re-running against the same `--out` directory overwrites each concept's
file in place and rebuilds every `index.md` from scratch; there's no
diff/merge, and objects removed from the bucket leave stale concept files
behind (pruning is not yet implemented).

## Development

```sh
go build ./...
go vet ./...
go test ./...
```
