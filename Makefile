.PHONY: build build-linux build-windows \
	build-windows-arm64 test vet fmt clean install \
	install-deps uninstall

BINARY := shmorby
SAMPLE_CONFIG := examples/shmorby.yaml

build:
	CGO_ENABLED=1 go build -o $(BINARY) ./cmd/shmorby

build-linux:
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
		go build -o shmorby ./cmd/shmorby

build-windows:
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
		go build -o shmorby.exe ./cmd/shmorby

build-windows-arm64:
	CGO_ENABLED=1 GOOS=windows GOARCH=arm64 \
		go build -o shmorby-windows-arm64.exe \
		./cmd/shmorby

test:
	CGO_ENABLED=1 go test ./... -v

vet:
	go vet ./...

fmt:
	go fmt ./...

clean:
	rm -f $(BINARY) $(BINARY).exe \
		shmorby-windows-arm64.exe

install-deps:
	./install.sh --deps-only

install:
	./install.sh

uninstall:
	./install.sh --uninstall
