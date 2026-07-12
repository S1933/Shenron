.PHONY: build test lint vet fmt-check clean

build:
	go build -o shenron ./cmd/shenron/

test:
	go test ./...

lint:
	golangci-lint run

vet:
	go vet ./...

fmt-check:
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || (echo "files need gofmt" && exit 1)

clean:
	rm -f shenron
