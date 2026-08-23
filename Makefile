VERSION ?= 0.2.0

.PHONY: build test vet assemble clean

build:
	mkdir -p bin
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/codex-remote ./cmd/codex-remote

test:
	go test ./...

vet:
	go vet ./...

assemble: test vet
	./scripts/assemble.sh $(VERSION)

clean:
	rm -rf bin dist
