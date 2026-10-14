.PHONY: generate build test test-db lint migrate-up migrate-down migrate-status run db-up db-down

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
