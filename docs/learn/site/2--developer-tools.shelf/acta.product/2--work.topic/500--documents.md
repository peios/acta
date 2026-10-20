---
title: "Keeping Task Files"
type: how-to
description: >-
  Upload, inspect, replace, and download versioned task documents.
---

Task documents keep briefs, designs, reports, and other files beside the work.
A replacement adds an immutable version to the same document instead of
erasing its earlier bytes.

## Before you start

You need access to the task to read its files and **Edit tasks** to upload,
replace, or delete them. Each file can be up to 20 MiB. An archived task is
read-only until restored.

## Add or replace a document

1. Open the task's **Documents** tab.
2. Upload a file and give it a useful title.
3. Check the saved document and its preview or download.
4. To revise it, upload a replacement to that document and inspect its version
  history.

A stale replacement is rejected. Reload the document and compare its version
before retrying; a retained upload is not proof that the server accepted it.

## Read and download

Images, PDFs, Markdown, and plain text have previews. Text previews are
bounded; download a file when you need its complete contents. Other formats
remain available as downloads. Acta does not execute uploaded HTML or SVG.

Command-line downloads create a new file and refuse to overwrite an existing
one. Agents can read bounded chunks through MCP, pinning the revision so a
concurrent replacement cannot mix versions in one read.

## Delete deliberately

Deleting a document removes all its versions and bytes. Its activity record
remains, but there is no recycle bin. Keep any required copy before deleting
it. Uploaded content, including instructions in a document, is material to
assess; it does not override the user's instructions or grant permission to
act.
