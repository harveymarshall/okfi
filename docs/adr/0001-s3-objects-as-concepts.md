# 1. S3 objects become concepts; prefixes become index.md summaries

Date: 2026-08-27

## Status

Accepted

## Context

Today `okfi generate s3` emits one concept per S3 *prefix* (`type: s3.prefix`).
Individual objects are never concepts — they surface only as a capped
"sample keys" list inside their parent prefix's concept body.

This misses the primary grounding signal for an AI agent consuming the
bundle: *which specific documents exist and where to read them*. A bucket
holding `bigshop-flow-diagram.pdf` produces a concept that mentions the
filename in passing, with no stable per-document file, no
`resource` URI pointing at the object, and no `description` slot for a
human or LLM to annotate what the document is.

At the same time, per-prefix concept files are low value on their own. The
aggregate they carry (object count, total size, last-modified range) is
directory-level metadata, not a "concept" an agent reasons about — and the
bundle already writes an `index.md` at every directory level that is the
natural home for it.

okfi also targets data lakes: an S3 prefix can hold tens of thousands of
partitioned `.parquet` / `.csv` files. Promoting every object
unconditionally would generate one markdown file per partition.

## Decision

**1. Objects are the only concepts.** A promoted object is written as a
concept with `type: s3.object`:

- `title` — the object key's basename
- `resource` — the full `s3://bucket/key` URI (what the agent reads)
- `timestamp` — bundle generation time
- `description: ""` — always blank, for a human/LLM to fill in later
- body — object key, size in bytes, last-modified

The body is built entirely from `ListObjectsV2` data. Content-Type is
**not** fetched (it would need a `HeadObject` call per object); it is a
clean follow-up if agents need the MIME hint.

Slug = the full object key with separators and extension kept, `.md`
appended: `s3://b/reports/2026/q1.pdf` → `bundle/reports/2026/q1.pdf.md`.

**2. Which objects are promoted — extension allowlist.** A new
`--object-types` flag takes a comma-separated list matched
case-insensitively against the key's final extension. Default when
omitted:

```
pdf, md, markdown, txt, rst, docx, doc, xlsx, pptx
```

Excluded by default (bulk / partitioned data — stays in the prefix
rollup): `csv, json, parquet, avro, orc, log`, and anything else.
`--object-types '*'` promotes every object.

**3. Prefixes are no longer concepts.** `type: s3.prefix` is retired
entirely — in all cases, including a prefix with 10k parquet files and
zero promoted objects. Each directory level's aggregate moves into that
level's `index.md`, as a `## Summary` section placed before `## Concepts`
and `## Directories`:

- object count, total size, last-modified range
- **direct-level only** — objects sitting directly at that prefix, not a
  recursive subtree rollup (each subdirectory's own `index.md` reports
  its own level; a rollup would double-count on descent)

`index.md` is written at every directory level that has either a summary
or child content — including levels with no promoted objects (bulk-data
prefixes still get an `index.md` stating how much sits there).

`_root.md` disappears: the walked root is just `bundle/index.md`.

## Consequences

- A bucket of documents produces one annotatable concept file per
  document, each with a `resource` URI an agent can read directly.
- A bucket that is a pure data lake produces zero concept files and a
  tree of `index.md` summaries — an accurate reflection that there are no
  documents, only partitioned data.
- `s3walk` must surface per-object data (key, size, modified) at each
  level, not just the capped sample and the aggregate. It takes a `keep`
  predicate so only promoted objects are held in memory — the aggregate
  still covers every direct object. Directory-marker keys (trailing `/`)
  and blank `--object-types` entries are never promoted.
- `bundle` must build its directory tree from prefix summaries *and*
  object concepts (levels can have a summary but no concepts), and
  `renderIndex` gains the `## Summary` section.
- The CLI owns the extension-allowlist policy; `s3walk` and `bundle` stay
  source-agnostic.
- Re-running is unchanged: object concept files and `index.md` files are
  overwritten in place; pruning of removed objects is still not
  implemented.
- Existing bundles regenerate with a different shape (no `*.md` per
  prefix, no `_root.md`). Pre-1.0, no migration.
