.PHONY: proto


local: version
	go build -o bin/runpodctl .
	go build -o bin/configure-github-action ./cmd/configure-github-action

release: buildall strip compress

buildall: android-arm64 linux-amd64 darwin-arm64 windows-amd64 windows-arm64 darwin-amd64

compress:
	upx --best bin/* || true
strip:
	strip bin/* || true

 	
android-arm64: version
	env GOOS=android GOARCH=arm64 go build -o bin/runpodctl-android-arm64 .
	env GOOS=android GOARCH=arm64 go build -o bin/configure-github-action-android-arm64 ./cmd/configure-github-action 
linux-amd64: version
	env GOOS=linux GOARCH=amd64 go build -o bin/runpodctl-linux-amd64 .
	env GOOS=linux GOARCH=amd64 go build -o bin/configure-github-action-linux-amd64 ./cmd/configure-github-action
darwin-arm64: version
	env GOOS=darwin GOARCH=arm64 go build -o bin/runpodctl-darwin-arm64 .
	env GOOS=darwin GOARCH=arm64 go build -o bin/configure-github-action-darwin-arm64 ./cmd/configure-github-action
windows-amd64: version
	env GOOS=windows GOARCH=amd64 go build -o bin/runpodctl-windows-amd64.exe .
	env GOOS=windows GOARCH=amd64 go build -o bin/configure-github-action-windows-amd64.exe ./cmd/configure-github-action
windows-arm64: version
	env GOOS=windows GOARCH=arm64 go build -o bin/runpodctl-windows-arm64.exe .
	env GOOS=windows GOARCH=arm64 go build -o bin/configure-github-action-windows-arm64.exe ./cmd/configure-github-action
darwin-amd64: version
	env GOOS=darwin GOARCH=amd64 go build -o bin/runpodctl-darwin-amd64 .
	env GOOS=darwin GOARCH=amd64 go build -o bin/configure-github-action-darwin-amd64 ./cmd/configure-github-action

lint: version
	golangci-lint run
version:
	echo "1.0.0-test" > VERSION