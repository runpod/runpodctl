.PHONY: proto

build-linux:
	env GOOS=linux GOARCH=amd64 go build -ldflags "-X 'main.Version=1.0.0'" -o bin/runpodctl .

dev:
	env GOOS=darwin GOARCH=arm64 go build -ldflags "-X 'main.Version=1.0.0'" -o bin/runpodctl .
windows:
	env GOOS=windows GOARCH=amd64 go build -ldflags "-X 'main.Version=1.0.0'" -o bin/runpodctl.exe .

dev-amd64:
	env GOOS=darwin GOARCH=amd64 go build -ldflags "-X 'main.Version=1.0.0'" -o bin/runpodctl .


lint:
	golangci-lint run
