set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-load := true

test-go:
	go -C pasta test --race ./...
	go -C demo/backend test --race ./...

test-js: demo-node-deps
	npm --prefix demo test
	npm --prefix demo run test:e2e

test: test-go test-js

coverage:
	go -C pasta test ./... --race -coverprofile=../coverage.out -coverpkg=./...

vet:
	go -C pasta vet ./...
	go -C demo/backend vet ./...

tidy:
	go -C pasta mod tidy
	GOWORK=off go -C demo/backend mod tidy
	go work sync

lint:
  golangci-lint run ./pasta/... ./demo/backend/...

demo-node-deps:
	if [ ! -d demo/node_modules/playwright-core ] || [ ! -d demo/node_modules/typescript ]; then npm --prefix demo ci; fi

demo-build: demo-node-deps
	npm --prefix demo run prepare:demo

demo-serve: demo-build
	npm --prefix demo run serve

