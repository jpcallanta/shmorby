.PHONY: build test vet fmt clean

BINARY := shmorby

build:
	CGO_ENABLED=1 go build -o $(BINARY) ./cmd/shmorby

test:
	CGO_ENABLED=1 go test ./... -v

vet:
	go vet ./...

fmt:
	go fmt ./...

clean:
	rm -f $(BINARY)
