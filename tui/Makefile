build:
	go build -o agrisense -ldflags="-s -w" .

build-all:
	GOOS=linux   GOARCH=amd64 go build -o "dist/Agrisense (Linux x64)"   -ldflags="-s -w" .
	GOOS=darwin  GOARCH=amd64 go build -o "dist/Agrisense (Intel Mac)"  -ldflags="-s -w" .
	GOOS=darwin  GOARCH=arm64 go build -o "dist/Agrisense (Silicon Mac)"  -ldflags="-s -w" .
	GOOS=windows GOARCH=amd64 go build -o "dist/Agrisense (Windows).exe"  -ldflags="-s -w" .

run:
	go run .

clean:
	rm -f agrisense dist/agrisense-*

.PHONY: build build-all run clean
