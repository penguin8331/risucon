#!/bin/sh
set -ex
cd `dirname $0`

RISUCON_DB_HOST=${RISUCON_DB_HOST:-127.0.0.1}
RISUCON_DB_PORT=${RISUCON_DB_PORT:-3306}
RISUCON_DB_USER=${RISUCON_DB_USER:-risucon}
RISUCON_DB_PASSWORD=${RISUCON_DB_PASSWORD:-risucon}
RISUCON_DB_NAME=${RISUCON_DB_NAME:-risucontest}

mysql -u"$RISUCON_DB_USER" \
		-p"$RISUCON_DB_PASSWORD" \
		--host "$RISUCON_DB_HOST" \
		--port "$RISUCON_DB_PORT" \
		"$RISUCON_DB_NAME" < 00_schema.sql

mysql -u"$RISUCON_DB_USER" \
		-p"$RISUCON_DB_PASSWORD" \
		--host "$RISUCON_DB_HOST" \
		--port "$RISUCON_DB_PORT" \
		"$RISUCON_DB_NAME" < 01_initial_data.sql
