set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-load := true

test:
	go test ./pasta/... --race -count=1

coverage:
	go test ./pasta/... -coverprofile=coverage.out -coverpkg=./...

vet:
	go vet ./pasta/...

tidy:
	go -C pasta mod tidy
	go work sync

