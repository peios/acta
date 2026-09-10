#!/bin/sh
set -eu
umask 077
# Compose bind-backed secrets retain host ownership. Only this short bootstrap
# runs as root; the server runs as 10001 without capabilities.
mkdir -p /run/acta
for name in database-url security-key backup-token; do
  cp "/run/secrets/$name" "/run/acta/$name"
  chmod 600 "/run/acta/$name"
done
chown -R 10001:10001 /run/acta
export ACTA_DATABASE_URL="$(cat /run/acta/database-url)"
export ACTA_SECURITY_KEY_FILE=/run/acta/security-key
export ACTA_BACKUP_TOKEN_FILE=/run/acta/backup-token
exec gosu 10001:10001 "$@"
