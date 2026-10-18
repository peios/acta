---
title: "Release Trust"
type: concept
description: >-
  Understand signed release manifests, pinned trust roots, compatibility
  checks, and publication gates.
---

The updater trusts an operator-installed verification key and repository
identity. Release metadata alone cannot replace that trust root.

`acta-release.json` is an Ed25519-signed envelope. Its base64 payload contains
the version, monotonically increasing release sequence, repository, updater
protocol, deployment layout, PostgreSQL major version, supported starting
schema, target schema, notes, and exact image digests for app, db, backup,
caddy and updater. The verifier pins the operator-installed public key and
configured repository. Neither a GitHub response nor the application can
replace that trust root. Replayed/older releases, unsupported
layouts/protocols, wrong namespaces, and unsupported schema transitions are
rejected. PostgreSQL major upgrades are not performed by this updater. Layout
or updater-protocol changes require an explicit operator deployment upgrade;
normal compatible updater image changes are handled by a detached replacement
helper after application cutover is durably complete.

The release channel is **peios/acta**, with separate
`ghcr.io/peios/acta-{server,db,backup,updater}` packages. The `app` manifest
entry uses the `acta-server` image package. Publishing remains disabled until
the GitHub repository variable `ACTA_RELEASE_ENABLED=true` is explicitly set
after release qualification. The packaging script requires the same
environment gate. Workflow dispatch accepts stable and prerelease versions.
Stable releases are marked latest only after the source checks, signed image
validation and update/recovery rehearsal pass.

Deployment layout 2 uses the final executable, database and backup-stanza
names. Layout-1 deployments are not accepted for in-place layout-2 updates.
The updater does not convert volume names, credentials, encryption domains, or
database names.

The repository workflow uses `ACTA_RELEASE_SIGNING_SEED` to sign the manifest.
The matching public key is `deploy/update/release.pub`. The private seed is
never shipped in an image. Maintain a secure independent backup of the signing
seed; rotate verification keys through a separately authenticated operator
procedure. The workflow builds Linux amd64 images initially. Do not install
these on ARM.

GitHub packages for a private repository require registry authentication in
the operator's Docker config. Reading private release assets also requires a
GitHub credential with access to the repository; configure `github_token_file`
in the updater config. Keep both outside the checkout. Release credentials are
sent only to GitHub's API and stripped from redirected asset downloads.
