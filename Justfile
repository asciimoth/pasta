set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-load := true

test:
	go -C pasta test ./... --race -count=1
	go -C demo test ./... --race -count=1

coverage:
	go -C pasta test ./... --race -coverprofile=../coverage.out -coverpkg=./...

vet:
	go -C pasta vet ./...
	go -C demo vet ./...

tidy:
	go -C pasta mod tidy
	go -C demo mod tidy
	go work sync

lint:
  golangci-lint run ./pasta/... ./demo/...

demo-build:
	GOOS=js GOARCH=wasm go -C demo build -o app.wasm .
	if [ -f "$(go env GOROOT)/misc/wasm/wasm_exec.js" ]; then \
		cp -f "$(go env GOROOT)/misc/wasm/wasm_exec.js" demo/; \
	elif [ -f "$(go env GOROOT)/lib/wasm/wasm_exec.js" ]; then \
		cp -f "$(go env GOROOT)/lib/wasm/wasm_exec.js" demo/; \
	else \
		echo "wasm_exec.js not found in GOROOT" >&2; \
		exit 1; \
	fi

demo-serve: demo-build
	python3 -m http.server 8000 --directory demo

demo-clean:
	rm -f demo/app.wasm demo/wasm_exec.js
