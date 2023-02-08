.PHONY: proto

dev:
	env GOOS=darwin GOARCH=arm64 go build -ldflags "-X 'main.Version=1.0.0'" -o bin/runpodctl .
lint:
	golangci-lint run
