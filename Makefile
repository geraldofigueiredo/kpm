BINARY     := kpm
MODULE     := github.com/geraldofigueiredo/kportmaster
CMD        := ./cmd/kpm
INSTALL_DIR := /usr/local/bin

VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS    := -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build install uninstall clean

build:
	go build $(LDFLAGS) -o $(BINARY) $(CMD)

install: build
	@echo "Installing $(BINARY) to $(INSTALL_DIR)..."
	@install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "Done. Run '$(BINARY) version' to verify."

uninstall:
	@echo "Removing $(BINARY) from $(INSTALL_DIR)..."
	@rm -f $(INSTALL_DIR)/$(BINARY)
	@echo "Done."

clean:
	@rm -f $(BINARY)
