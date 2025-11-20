.PHONY: proto

local:
	@COMMIT=$$(git rev-parse HEAD 2>/dev/null || echo "unknown"); \
	go build -mod=mod -ldflags "-s -w -X main.Version=dev-$$COMMIT" -o bin/runpodctl .

release: buildall strip compress

buildall: android-arm64 linux-amd64 darwin-arm64 windows-amd64 windows-arm64 darwin-amd64

compress:
	upx --best bin/* || true
strip:
	strip bin/* || true

 	
android-arm64:
	@VERSION=$$(git describe --tags --exact-match HEAD 2>/dev/null || git describe --tags 2>/dev/null || echo "unknown"); \
	COMMIT=$$(git rev-parse HEAD 2>/dev/null || echo "unknown"); \
	env CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -mod=mod -ldflags "-s -w -X main.Version=$$VERSION-$$COMMIT" -o bin/runpodctl-android-arm64 .
linux-amd64:
	@VERSION=$$(git describe --tags --exact-match HEAD 2>/dev/null || git describe --tags 2>/dev/null || echo "unknown"); \
	COMMIT=$$(git rev-parse HEAD 2>/dev/null || echo "unknown"); \
	env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=mod -ldflags "-s -w -X main.Version=$$VERSION-$$COMMIT" -o bin/runpodctl-linux-amd64 .
darwin-arm64:
	@VERSION=$$(git describe --tags --exact-match HEAD 2>/dev/null || git describe --tags 2>/dev/null || echo "unknown"); \
	COMMIT=$$(git rev-parse HEAD 2>/dev/null || echo "unknown"); \
	env CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -mod=mod -ldflags "-s -w -X main.Version=$$VERSION-$$COMMIT" -o bin/runpodctl-darwin-arm64 .
windows-amd64:
	@VERSION=$$(git describe --tags --exact-match HEAD 2>/dev/null || git describe --tags 2>/dev/null || echo "unknown"); \
	COMMIT=$$(git rev-parse HEAD 2>/dev/null || echo "unknown"); \
	env CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -mod=mod -ldflags "-s -w -X main.Version=$$VERSION-$$COMMIT" -o bin/runpodctl-windows-amd64.exe .
windows-arm64:
	@VERSION=$$(git describe --tags --exact-match HEAD 2>/dev/null || git describe --tags 2>/dev/null || echo "unknown"); \
	COMMIT=$$(git rev-parse HEAD 2>/dev/null || echo "unknown"); \
	env CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -mod=mod -ldflags "-s -w -X main.Version=$$VERSION-$$COMMIT" -o bin/runpodctl-windows-arm64.exe .
darwin-amd64:
	@VERSION=$$(git describe --tags --exact-match HEAD 2>/dev/null || git describe --tags 2>/dev/null || echo "unknown"); \
	COMMIT=$$(git rev-parse HEAD 2>/dev/null || echo "unknown"); \
	env CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -mod=mod -ldflags "-s -w -X main.Version=$$VERSION-$$COMMIT" -o bin/runpodctl-darwin-amd64 .