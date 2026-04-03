DB_CONTAINER ?= personal-agent-db-1
DB_USER ?= postgres
DB_NAME ?= agentdb
SCHEMA_FILE ?= /sql/schema.sql

reset-db:
	podman exec $(DB_CONTAINER) psql \
		--username "$(DB_USER)" \
		--dbname "postgres" \
		-v ON_ERROR_STOP=1 \
		-c "DROP DATABASE IF EXISTS $(DB_NAME) WITH (FORCE);"
	podman exec $(DB_CONTAINER) psql \
		--username "$(DB_USER)" \
		--dbname "postgres" \
		-v ON_ERROR_STOP=1 \
		-c "CREATE DATABASE $(DB_NAME);"
	podman exec $(DB_CONTAINER) psql \
		--username "$(DB_USER)" \
		--dbname "$(DB_NAME)" \
		-v ON_ERROR_STOP=1 \
		-f "$(SCHEMA_FILE)"
