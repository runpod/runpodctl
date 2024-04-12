.PHONY: proto

local:
	go build -ldflags "-X 'main.Version=1.0.0'" -o bin/runpodctl .

linux:
	env GOOS=linux GOARCH=amd64 go build -ldflags "-X 'main.Version=1.0.0'" -o bin/runpodctl .
mac:
	env GOOS=darwin GOARCH=arm64 go build -ldflags "-X 'main.Version=1.0.0'" -o bin/runpodctl .
windows:
	env GOOS=windows GOARCH=amd64 go build -ldflags "-X 'main.Version=1.0.0'" -o bin/runpodctl.exe .
mac-amd64:
	env GOOS=darwin GOARCH=amd64 go build -ldflags "-X 'main.Version=1.0.0'" -o bin/runpodctl .


lint:
	golangci-lint run
