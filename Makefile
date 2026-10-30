BIN := bin
GO  ?= go

.PHONY: all tui web server desktop test fmt vet clean

all: tui web server desktop

tui:
	@mkdir -p $(BIN)
	$(GO) build -trimpath -o $(BIN)/utub ./cmd/utub

web:
	@echo "not yet — M5 (Vue 3 SPA in web/)"

server:
	@echo "not yet — M4 (internal/api + cmd/utubd)"

desktop:
	@echo "not yet — M6 (Wails v3 shell in desktop/)"

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

clean:
	rm -rf $(BIN) web/dist web/node_modules
