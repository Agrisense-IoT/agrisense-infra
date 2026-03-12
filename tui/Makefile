build:
	go build -o agrisense -ldflags="-s -w" .

build-all:
	GOOS=linux   GOARCH=amd64 go build -o "dist/agrisense-linux"   -ldflags="-s -w" .
	GOOS=darwin  GOARCH=amd64 go build -o "dist/agrisense-intel"  -ldflags="-s -w" .
	GOOS=darwin  GOARCH=arm64 go build -o "dist/agrisense-silicon"  -ldflags="-s -w" .
	lipo -create -output "dist/agrisense-mac" "dist/agrisense-intel" "dist/agrisense-silicon"
	rm "dist/agrisense-intel" "dist/agrisense-silicon"
	GOOS=windows GOARCH=amd64 go build -o "dist/agrisense-windows.exe"  -ldflags="-s -w" .

run:
	go run .

clean:
	rm -f agrisense dist/agrisense-*

.PHONY: build build-all run clean
