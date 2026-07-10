.PHONY: build test lint clean

build:
	go build -o agents-sync ./cmd/agents-sync/

test:
	go test ./...

lint:
	golangci-lint run

clean:
	rm -f agents-sync
