.PHONY: build build-windows build-linux test vet fmt clean install

BINARY := shmorby
SAMPLE_CONFIG := examples/shmorby.yaml

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

install:
ifeq ($(OS),Windows_NT)
	@mkdir -p "$(LOCALAPPDATA)\\shmorby"
	go build -o "$(LOCALAPPDATA)\\shmorby\\$(BINARY).exe" ./cmd/shmorby
	@if exist "$(LOCALAPPDATA)\\shmorby\\config.yaml" copy "$(LOCALAPPDATA)\\shmorby\\config.yaml" "$(LOCALAPPDATA)\\shmorby\\config.yaml.bak"
	@if exist "$(LOCALAPPDATA)\\shmorby\\config.yaml" echo Backed up existing config to config.yaml.bak
	copy /Y "$(SAMPLE_CONFIG)" "$(LOCALAPPDATA)\\shmorby\\config.yaml"
else
	@mkdir -p ~/.config/shmorby
	go build -o $(BINARY) ./cmd/shmorby
	sudo install $(BINARY) /usr/local/bin/$(BINARY)
	@if [ -f ~/.config/shmorby/config.yaml ]; then \
		backup=~/.config/shmorby/config.yaml.bak.$$(date +%Y%m%d%H%M%S); \
		cp ~/.config/shmorby/config.yaml $$backup; \
		echo "Backed up existing config to $$backup"; \
	fi; \
	cp $(SAMPLE_CONFIG) ~/.config/shmorby/config.yaml
endif
