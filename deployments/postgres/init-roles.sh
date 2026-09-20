#!/usr/bin/env bash
# Creates least-privilege roles for the challenge database.
# Runs once via /docker-entrypoint-initdb.d. Passwords come from the
# environment (Compose), never from migrations or the repo.
set -euo pipefail

: "${OWNER_PASSWORD:?OWNER_PASSWORD is required}"
: "${APP_PASSWORD:?APP_PASSWORD is required}"

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<EOSQL
DO \$\$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'owner') THEN
        CREATE ROLE owner LOGIN PASSWORD '${OWNER_PASSWORD}';
    END IF;
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'app') THEN
        CREATE ROLE app LOGIN PASSWORD '${APP_PASSWORD}';
    END IF;
END
\$\$;
SELECT 'CREATE DATABASE wallet OWNER owner'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'wallet')\gexec
EOSQL
