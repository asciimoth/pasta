set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-load := true

test:
	go -C pasta test ./... --race -count=1

coverage:
	go -C pasta test ./... --race -coverprofile=../coverage.out -coverpkg=./...

vet:
	go -C pasta vet ./...

tidy:
	go -C pasta mod tidy
	go work sync
