.PHONY: build test test-race lint vet fmt-check vuln clean

build:
	go build -o shenron ./cmd/shenron/

test:
	go test ./...

test-race:
	go test -race -shuffle=on ./...

vuln:
	GOTOOLCHAIN=go1.25.12 go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

lint:
	golangci-lint run

vet:
	go vet ./...

fmt-check:
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || (echo "files need gofmt" && exit 1)

clean:
	rm -f shenron
