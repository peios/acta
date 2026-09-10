#!/bin/sh
set -eu
umask 077
mkdir -p /run/acta-db
for name in postgres-password app-password; do
  cp "/run/secrets/$name" "/run/acta-db/$name"
  chmod 600 "/run/acta-db/$name"
done
chown -R postgres:postgres /run/acta-db
export POSTGRES_PASSWORD_FILE=/run/acta-db/postgres-password
exec /usr/local/bin/docker-entrypoint.sh "$@"
