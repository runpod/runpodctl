.PHONY: proto

dev:
	env GOOS=windows GOARCH=amd64 go build -ldflags "-X 'main.Version=1.0.0'" -o bin/runpodctl.exe .
lint:
	golangci-lint run
