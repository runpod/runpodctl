.PHONY: proto


local: version
	go build -o bin/runpodctl .

release: buildall strip compress

buildall: android-arm64 linux-amd64 darwin-arm64 windows-amd64 windows-arm64 darwin-amd64

compress:
	upx --best bin/* || true
strip:
	strip bin/* || true

 	
android-arm64: version
	env GOOS=android GOARCH=arm64 go build -o bin/runpodctl-android-arm64 .
linux-amd64: version
	env GOOS=linux GOARCH=amd64 go build -o bin/runpodctl-linux-amd64 .
darwin-arm64: version
	env GOOS=darwin GOARCH=arm64 go build -o bin/runpodctl-darwin-arm64 .
windows-amd64: version
	env GOOS=windows GOARCH=amd64 go build -o bin/runpodctl-windows-amd64.exe .
windows-arm64: version
	env GOOS=windows GOARCH=arm64 go build -o bin/runpodctl-windows-arm64.exe .
darwin-amd64: version
	env GOOS=darwin GOARCH=amd64 go build -o bin/runpodctl-darwin-amd64 .

lint: version
	golangci-lint run
version:
	echo "1.0.0-test" > VERSION