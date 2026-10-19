.PHONY: all build build-web test clean

PLUGIN_NAME := cpa-quota

UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
	EXT := dylib
else ifeq ($(OS),Windows_NT)
	EXT := dll
else
	EXT := so
endif

all: build

build-web:
	@echo "==> Building Web Frontend with pnpm & Vite..."
	@cd web && pnpm install && pnpm run build

build: build-web
	@echo "==> Compiling CGO Shared Library ($(PLUGIN_NAME).$(EXT))..."
	CGO_ENABLED=1 go build -buildmode=c-shared -ldflags="-s -w" -o $(PLUGIN_NAME).$(EXT) .
	@rm -f $(PLUGIN_NAME).h
	@echo "==> Build successful: $(PLUGIN_NAME).$(EXT)"

test:
	@echo "==> Running backend tests..."
	go test ./...

clean:
	@rm -f $(PLUGIN_NAME).so $(PLUGIN_NAME).dylib $(PLUGIN_NAME).dll $(PLUGIN_NAME).h
	@rm -rf web/dist
