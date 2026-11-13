GOCMD=go
# -trimpath keeps build-machine paths out of the binaries; -s -w drops the
# symbol table and debug info. Stack traces still carry function names and lines.
GOBUILD=$(GOCMD) build -trimpath -ldflags="-s -w"
GOOS?=windows
GOARCH?=amd64
GOTARGETENV=GOOS=$(GOOS) GOARCH=$(GOARCH)
# The shared-memory reader is cgo; cross-compiling from Linux/WSL needs mingw-w64.
WINCC?=x86_64-w64-mingw32-gcc
CGOWINENV=CGO_ENABLED=1 CC=$(WINCC)

# dump: build the Windows dumper used to validate the parser against a live AIDA64.
dump:
	$(GOTARGETENV) $(CGOWINENV) $(GOBUILD) -o build/aida64dump.exe ./cmd/aida64dump

# bridge: build the Windows AIDA64 bridge (cgo shared-memory reader + go-plugin gRPC).
bridge:
	$(GOTARGETENV) $(CGOWINENV) $(GOBUILD) -o build/aida64-bridge.exe ./cmd/aida64-bridge

# mock: build the mock bridge (no cgo; cross-compiled for the Windows plugin host
# but also runs natively for UI development on this machine).
mock:
	$(GOTARGETENV) $(CGOWINENV) $(GOBUILD) -o build/mock-bridge.exe ./cmd/mock-bridge

# host: build the Stream Deck plugin host (spawns aida64-bridge.exe, serves the
# WebSocket Stream Deck speaks to). Pure Go (no cgo), so it cross-compiles clean.
host:
	$(GOTARGETENV) $(GOBUILD) -o build/aida64.exe ./cmd/aida64

# build: compile everything for the target.
build: dump bridge mock host

# test runs the cross-platform suites natively (parser + bridge service).
test:
	$(GOCMD) test ./...

# vet: static checks for both native and the Windows cgo cross-compile.
vet:
	$(GOCMD) vet ./...
	$(GOTARGETENV) $(CGOWINENV) $(GOCMD) vet ./...

SDPLUGIN=com.poizenjam.aida64sd.sdPlugin

# streamdeck CLI packs and validates the plugin. A raw `zip` of the folder
# contents is NOT a valid artifact: Stream Deck requires the files to sit
# under a top-level <uuid>.sdPlugin/ folder, which the CLI produces. See the
# flat-zip install failure (Unable to install, 2026-10-31).
STREAMDECK_BIN ?= $(shell command -v streamdeck 2>/dev/null || echo streamdeck)

# package: assemble the .streamDeckPlugin artifact via the official CLI.
# It ships the host + the real bridge + the font. It never ships the mock
# bridge, Linux binaries, or dev logs (.sdignore drives streamdeck pack).
package: host bridge
	test -x $(STREAMDECK_BIN) || { echo "streamdeck CLI not found; npm install -g @elgato/cli"; exit 1; }
	rm -rf dist && mkdir -p dist
	cp -r $(SDPLUGIN) dist/
	cp build/aida64.exe build/aida64-bridge.exe dist/$(SDPLUGIN)/
	$(STREAMDECK_BIN) pack dist/$(SDPLUGIN) --output dist --force
	mv dist/com.poizenjam.aida64sd.streamDeckPlugin com.poizenjam.aida64sd.streamDeckPlugin
	rm -rf dist
	@echo "wrote com.poizenjam.aida64sd.streamDeckPlugin"
