#!/bin/sh
set -eu
# Password is a psql quoted literal, never interpolated into SQL syntax.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  --set=app_password="$(cat /run/acta-db/app-password)" <<'SQL'
CREATE ROLE acta LOGIN PASSWORD :'app_password' NOSUPERUSER NOCREATEDB NOCREATEROLE;
CREATE DATABASE acta OWNER acta;
SQL
