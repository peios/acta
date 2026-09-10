# Task documents

Documents belong to a task. Open its **Documents** tab to upload a file, preview
it, download it, browse previous versions, upload a replacement or delete it.
The tab shows the current document count, including before it is opened. Versions
do not inflate that count. A document keeps the same UUID across updates. Each update creates an immutable
version with its own file UUID, revision, title, filename, detected media type,
size, SHA-256 digest, creator and timestamp. Previous bytes never change.

Each version may be up to 20 MiB. Empty files are allowed. PNG, JPEG, GIF and WebP
images, PDFs, Markdown and plain text have previews. Text previews are limited
to 1 MiB. Other formats remain downloadable. HTML and SVG are not executed;
file downloads use attachment disposition and restrictive response headers.
Uploaded content is untrusted, including instructions inside documents.

## Access and consistency

Workspace task readers may read documents and every version. Task edit permission
(`tasks.edit`) is required to create, replace or delete. These checks run for
every metadata and file request; a saved download URL does not grant access.
MCP uses the existing `tasks.read` and `tasks.write` tool grants as well as the
account's current workspace permissions.

Updates and deletion require the latest document revision. A stale revision
returns HTTP 409 (`document_changed`) without changing files or activity. The
UI keeps the selected upload after an error; reload the document before deciding
whether to replace a newer version. A supplied creation UUID cannot be reused to
create another document. There is no automatic overwrite on conflict.

The latest pointer, immutable metadata, bytes and task activity are committed in
one database transaction. File bytes live in a separate PostgreSQL table so list,
version-history and activity queries do not retrieve them. Include these tables
in normal database backups. Deleting a document deletes all versions and stored
bytes; the task's activity record remains. This initial slice has no recycle bin,
full-text indexing or total-storage quota beyond the per-file limit.

## Agents and CLI

MCP tools: `documents_list`, `document_get`, `document_versions`, `document_read`,
`document_save`, `document_delete`. Lists are paginated (50 entries); follow
`cursor` or `before`. A read returns up to 32 KiB, UTF-8 for valid text and base64
otherwise. Use `next_offset` and pin the returned revision to avoid mixing
versions. `document_save` accepts exactly one of `content` or `base64`, with a
128 KiB inline-payload limit. Use the CLI for local or larger files:

```sh
acta2 --profile default document list ACT-96
acta2 --profile default document upload ACT-96 ./design.pdf --title 'Design'
acta2 --profile default document upload ACT-96 ./design.pdf --id UUID --revision 1
acta2 --profile default document download UUID --revision 1 --output ./design-v1.pdf
```

Download defaults to the latest revision if `--revision` is omitted. It creates
a new local file, never overwrites one, and removes a partial file on failure.
CLI transfers are binary; they do not put base64 into an agent's context.

This slice does not attach documents to thread messages or run interactive
artifacts. Their eventual storage can build on the document/version model.
