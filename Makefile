.PHONY: build test lint
build:
	go build -o bin/plinth ./cmd/plinth
test:
	go test ./... -race -count=1
lint:
	go vet ./... && test -z "$$(gofmt -l .)"
