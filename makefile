.PHONY: proto

build-linux:
	env GOOS=linux GOARCH=amd64 go build -ldflags "-X 'main.Version=1.0.0'" -o bin/runpod .

dev:
	env GOOS=darwin GOARCH=arm64 go build -ldflags "-X 'main.Version=1.0.0'" -o bin/runpod .
windows:
	env GOOS=windows GOARCH=amd64 go build -ldflags "-X 'main.Version=1.0.0'" -o bin/runpod.exe .

dev-amd64:
	env GOOS=darwin GOARCH=amd64 go build -ldflags "-X 'main.Version=1.0.0'" -o bin/runpod .


lint:
	golangci-lint run
