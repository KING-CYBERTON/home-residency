.PHONY: css css-watch generate build test test-db lint migrate-up migrate-down migrate-status run db-up db-down

DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/homend?sslmode=disable
TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/homend_test?sslmode=disable

generate:            ## regenerate the server interface and models from api/openapi.yaml
	go generate ./...

build:
	go build -o bin/api ./cmd/api

test:                ## unit tests (the database tests skip when TEST_DATABASE_URL is unset)
	go test ./...

test-db:             ## everything, including the end-to-end tests against Postgres
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race -count=1 ./...

lint:
	go vet ./...
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "gofmt needed" && exit 1)

migrate-up:
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/api migrate up

migrate-down:        ## roll back the latest migration
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/api migrate down

migrate-status:
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/api migrate status

run:
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/api serve

db-up:               ## local Postgres 16 with both databases
	docker compose up -d --wait db

db-down:
	docker compose down

# Tailwind standalone CLI (no Node needed). The built CSS is committed and embedded in the binary.
TAILWIND_VERSION ?= v4.1.14
TAILWIND_OS := $(shell uname -s | tr A-Z a-z | sed 's/darwin/macos/')
TAILWIND_ARCH := $(shell uname -m | sed 's/x86_64/x64/; s/aarch64/arm64/')
TAILWIND := bin/tailwindcss-$(TAILWIND_VERSION)

$(TAILWIND):
	mkdir -p bin
	curl -sSLf -o $@ https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-$(TAILWIND_OS)-$(TAILWIND_ARCH)
	chmod +x $@

css: $(TAILWIND)     ## rebuild internal/web/static/css/app.css after changing templates or styles
	$(TAILWIND) -i internal/web/styles/app.css -o internal/web/static/css/app.css --minify

css-watch: $(TAILWIND)
	$(TAILWIND) -i internal/web/styles/app.css -o internal/web/static/css/app.css --watch
