build:
	go build -o bin/nearby ./cmd/nearby

test:
	go test -race ./...

test-live:
	go test -tags integration ./internal/scraper/

clean:
	rm -rf bin

.PHONY: build test test-live clean
