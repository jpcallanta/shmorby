.PHONY: build build-windows build-linux test vet fmt clean

BINARY := shmorby

build:
	CGO_ENABLED=1 go build -o $(BINARY) ./cmd/shmorby

build-windows:
	# Requires C cross-compiler (mingw-w64) for SQLite.
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build -o shmorby.exe ./cmd/shmorby

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
