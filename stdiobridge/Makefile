.PHONY: build test clean install

VERSION ?= dev

build:
	go build -ldflags "-X main.version=$(VERSION)" -o stdiobridge .

test-client:
	go build -o test_client test_client.go

test: build test-client
	@echo "Make sure sidekick is running with: ./sidekick --sse --tui"
	@echo "Press Enter to continue..."
	@read dummy
	./test_client "./stdiobridge --sse-url http://localhost:5050/sse --verbose"

clean:
	rm -f stdiobridge test_client

install: build
	cp stdiobridge /usr/local/bin/

cross-compile:
	GOOS=darwin GOARCH=arm64 go build -ldflags "-X main.version=$(VERSION)" -o stdiobridge-darwin-arm64
	GOOS=darwin GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" -o stdiobridge-darwin-amd64
	GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" -o stdiobridge-linux-amd64
	GOOS=linux GOARCH=arm64 go build -ldflags "-X main.version=$(VERSION)" -o stdiobridge-linux-arm64
	GOOS=windows GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" -o stdiobridge-windows-amd64.exe