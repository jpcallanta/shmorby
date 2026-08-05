.PHONY: build build-linux test vet fmt clean install install-deps uninstall

BINARY := shmorby
SAMPLE_CONFIG := examples/shmorby.yaml

build:
	CGO_ENABLED=1 go build -o $(BINARY) ./cmd/shmorby

build-linux:
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o shmorby ./cmd/shmorby

test:
	CGO_ENABLED=1 go test ./... -v

vet:
	go vet ./...

fmt:
	go fmt ./...

clean:
	rm -f $(BINARY)

install-deps:
	./install.sh --deps-only

install:
	./install.sh

uninstall:
	./install.sh --uninstall
