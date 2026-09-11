# Acta naming and repository handover

The local rename prepares the new application to become Acta. GitHub repository
renames, publication and production deployment are separate steps and are not
performed by changing the source tree.

## Final names

- Product, PWA and MCP server: Acta.
- Go module: `acta`; npm package: `acta-web`.
- Executables: `acta` (CLI), `acta-server`, `acta-backup`, `acta-update`.
- CLI profile directory and keyring service: `acta`.
- Fresh deployment database and pgBackRest stanza: `acta`.
- Default Compose projects: `acta` (development), `acta-production` (deployment).
- Release repository: `peios/acta`.
- Container packages: `ghcr.io/peios/acta-{server,db,backup,updater}`.

The server image reuses `ghcr.io/peios/acta-server` by explicit user decision:
the only installation watching it was this VPS, whose updater is now disabled.
The Compose service and manifest key remain `app`. Signed manifests using the
earlier development layout are rejected:
layout 2 names different executables, database and backup stanza. Fresh deployment
is intentional; no conversion of preview data, browser storage, credential stores
or encryption domains is attempted. Existing legacy CLI installations must not be
assumed to share the new profile format.

## Handover boundary

Publishing requires the repository to be `peios/acta`, the main branch and the
explicit repository variable `ACTA_RELEASE_ENABLED=true`. The packaging script
requires the corresponding environment variable too. Leave the gate unset until
the repository handover, signing key, registry permissions and final image names
have been checked. The handover is complete: the old repository is
`peios/acta-legacy`, and the new repository is `peios/acta`. The release workflow
permits stable versions only with a compatible predecessor and successful real
update/recovery validation. `v0.1.0-rc.1` is the sole bootstrap exception: it runs
the recovery suite using a disposable same-image baseline for layout 2. Stable
`v0.1.0` must then pass a real upgrade from that candidate.

The existing checkout directory can retain its development name until repository
handover. Filesystem location does not determine product or release identity.
Protected legacy migration artifacts and the old instance on port 8085 are outside
this source rename. No VPS operation is part of this change.

## Local validation

The rename passed frontend build, formatting, Svelte/TypeScript and interaction
tests, Go vet/unit tests, and the full Go race suite with a disposable PostgreSQL
database. All four renamed executables and production Docker targets build.
The production Compose rehearsal passed HTTPS, OAuth origin, service isolation,
persistence across container recreation, encrypted backup and a real restore drill
using the new database and pgBackRest stanza names. The publishing gate rejects
unapproved execution, and release validation rejects unrelated image packages
and development layout. No image was published for this local validation.
