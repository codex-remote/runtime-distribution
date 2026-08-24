VERSION ?= 0.2.0-beta.3
CHANNEL ?= beta

.PHONY: build test vet notices assemble clean

build:
	mkdir -p bin
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/codex-remote ./cmd/codex-remote

test:
	go test ./...

vet:
	go vet ./...

notices:
	./scripts/generate-third-party-notices.sh

assemble: test vet
	./scripts/assemble.sh $(VERSION) $(CHANNEL)

clean:
	rm -rf bin dist
