# Image messages

The thread composer accepts pasted images, files dropped onto the composer, and
images selected with its attachment button. Up to four PNG, JPEG, GIF or WebP
images can be attached, with a combined limit of 4 MiB. Text remains limited to
64 KiB. Images can be sent with text or on their own; other file types and PDFs
are not part of this input slice.

The composer shows removable thumbnails before sending. Image bytes are saved
in IndexedDB and their small references are saved with the existing per-thread,
per-lane browser draft. Reloading retains the draft. A failed local storage read
cannot silently turn an image message into a text-only message. Confirmed sends
release their local image bytes; rejected sends preserve attachments for editing
and retry. An uncertain send keeps its original command UUID, even if image bytes
subsequently become unavailable. Browser drafts are local to that browser.

The server validates the count, aggregate decoded size, base64 encoding and
raster MIME signature before accepting a send. Image bytes and names become part
of the existing immutable send command. Reusing the UUID with changed image data
is rejected. Authentication, owner scoping and thread deletion apply exactly as
for text messages. The harness revalidates the message before provider delivery.
No remote image URL or provider-local path is fetched by this input feature.

The harness maps images to embedded `image` input URLs for Codex and base64
`image` content blocks for Claude. Text and images form one provider user message.
Codex's native model catalogue is used to reject a model that explicitly excludes
image input; the draft stays available to change models or remove attachments.
Acta never automatically changes a model. An absent capability field is not
interpreted as a claim that the model cannot accept images.

Normalized `message/user.content` supports text and raster image blocks. Its
persisted conversation item stores the image gallery as well as the text. Provider
echoes and command receipts reconcile against the original submission UUID.
Claude's lifecycle receipt includes the original persisted image content. Codex
may echo images as local paths; a correlated Acta submission supplies the original
bytes instead of making the server or browser open that path. Unknown shapes
remain inspectable through Unknown Frames.

Completed and pending message galleries use the same click-to-enlarge image
preview as tool results. Images remain in conversation history, independently of
browser draft storage and provider-local session files. Raw debug records retain
provider image data under the existing debug retention policy.

## Transport and deployment

Message HTTP requests and harness controls permit 8 MiB encoded payloads. The
provider pipe permits an 8 MiB input and a 16 MiB HTTP transport envelope to allow
JSON string escaping. These are bounded increases for embedded raster inputs.
Existing detached pipe processes retain the limits from their executable; they
advertise their input limit. The supervisor rejects an oversized image before
provider delivery if that pipe needs updating, preserving the draft. They must
be restarted to use the new input limit. Restarting a detached pipe closes
its child provider processes, so coordinate that restart and resume saved threads.

## Validation (2026-09-09)

Automated coverage includes aggregate validation, invalid/base64-spoofed formats,
immutable command identity, private persisted image history, image-only delivery,
provider-specific input formats, replay/receipt deduplication, and a 6 MiB pipe
write delivered only once across retry. Browser outbox tests cover missing files,
retries, new drafts preserved during old confirmation and local-storage failure.

Live scratch conversations verified image picker, reload persistence, delivery,
history and enlarged previews with Claude Haiku and Codex Luna. Both identified
an image's red and blue halves. Spark advertises text-only input and could not
interpret the initial probe; that became an explicit pre-send model-capability
rejection. The initial normal pipe was kept alive during these small-image tests.

Mobile browser checks also passed paste, drag-and-drop, removing one of several
attachments, unsupported-format/aggregate-size errors and image-only submission.
That exposed and fixed a Svelte-proxy/outbox cloning bug on the second attachment.
A 1.8 MiB PNG was delivered end to end through a freshly restarted isolated QA
pipe to Haiku and correctly interpreted; its gallery survived reload. Full Go
and PostgreSQL integration tests passed, as did focused race tests, all 26
frontend test files, Svelte checks and production build.
