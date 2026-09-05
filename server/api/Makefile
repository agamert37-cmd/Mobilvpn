.PHONY: build test vet fmt run clean

build:
	go build -o bin/vpn-api ./cmd/vpn-api

test:
	go test ./... -race

vet:
	go vet ./...

fmt:
	gofmt -l .

run:
	go run ./cmd/vpn-api

clean:
	rm -rf bin
