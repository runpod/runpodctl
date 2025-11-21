.PHONY: proto

# Git version and commit
VERSION = git describe --tags --exact-match HEAD 2>/dev/null || git describe --tags 2>/dev/null || echo "unknown"
COMMIT = git rev-parse HEAD 2>/dev/null || echo "unknown"

local:
	@COMMIT=$$($(COMMIT)); \
	go build -mod=mod -ldflags "-s -w -X main.Version=dev-$$COMMIT" -o bin/runpodctl .

release: buildall strip compress

buildall: android-arm64 linux-amd64 darwin-arm64 windows-amd64 windows-arm64 darwin-amd64

compress:
	upx --best bin/* || true

strip:
	strip bin/* || true

# Generic build function
define build-target
	@VERSION=$$($(VERSION)); \
	COMMIT=$$($(COMMIT)); \
	env CGO_ENABLED=0 GOOS=$(1) GOARCH=$(2) go build -mod=mod -ldflags "-s -w -X main.Version=$$VERSION-$$COMMIT" -o bin/runpodctl-$(1)-$(2)$(3) .
endef

# Platform-specific targets
android-arm64:
	$(call build-target,android,arm64,)

linux-amd64:
	$(call build-target,linux,amd64,)

darwin-arm64:
	$(call build-target,darwin,arm64,)

darwin-amd64:
	$(call build-target,darwin,amd64,)

windows-amd64:
	$(call build-target,windows,amd64,.exe)

windows-arm64:
	$(call build-target,windows,arm64,.exe)