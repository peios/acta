---
title: "Backup Policy and Health"
type: concept
description: >-
  Interpret scheduling, retention, restore evidence, log freshness, and
  independent backup alerts.
---

Backup health separates integrity checks, successful restore drills, and
archived-log freshness. A successful upload alone does not establish
recoverability.

Schedules are fixed intervals anchored to an explicit UTC timestamp, rather
than local wall-clock cron. Set the anchor to the desired first slot.
Daylight-saving changes do not shift that UTC cadence. Backup/full/drill
intervals and freshness alerts are configurable in minutes; missed slots
coalesce into one job. Full backups take precedence, then incrementals, then
drills. Failures back off five minutes. A saved job captures its policy and
keeps the same UUID through crash reconciliation. Jobs never overlap and
policy changes are refused while busy.

Retention counts full chains, bounded by operator minimum/maximum (minimum
two). No expiration occurs before a full backup passes a real restore. The
most recent restored full chain stays protected until a newer full passes;
unverified newer fulls prevent cleanup. Extra retention therefore consumes
space deliberately when drills fail. WAL retention follows retained full
chains. Monitor repository free space independently. Switching destination
changes future jobs, leaving previous repositories untouched; the operator
remains responsible for their retention.

A recovery point is complete only after the metadata consistency check;
integrity also requires a valid pgBackRest report, including every checked
data/WAL file. Upload success and exit status alone do not establish
integrity. A drill starts PostgreSQL from backup, validates cluster
identity/schema/key and document hashes, then invokes the **recorded
release's** read-only verifier, including account security-data decryption.
The drill tests the newest integrity-checked full backup. Standalone restores
can target any set, including incrementals and explicit times.

Health distinguishes last integrity-checked backup, last successful restore,
and last archived-WAL evidence. It does not invent a guaranteed latest
recoverable instant from one archive timestamp. Repository errors, stale
evidence, disabled schedules, failed jobs, and overdue drills are visible.
Jobs keep the latest 100 records. The settings page retains the latest 500
recovery points plus older full/restore evidence; the standalone `list`
command provides the complete catalog. Catalog inspection streams individual
records, discarding unneeded encrypted metadata rather than accumulating it in
memory. Protected pgBackRest/operator logs provide lower-level diagnostics.

`alert_command` can name an operator-owned executable. On health changes it
receives JSON on stdin (`kind: acta.backup.health`, `warnings`, `at`); failed
delivery is visible and retried after five minutes. This path works without
the Acta DB. Configure it before production and test its delivery. Also use an
external uptime monitor for the backup service/host itself: no process can
report its own death. Health inspection runs between bounded jobs, so size job
timeouts and the external monitor's deadline for the installation. State write
errors fail closed and cause service restart; policy is never acknowledged
before its atomic durable write.
