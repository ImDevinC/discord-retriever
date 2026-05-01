.PHONY: build clean test run

BINARY=discord-retriever

build:
	go build -o $(BINARY) .

clean:
	rm -f $(BINARY)
	rm -rf output/

test:
	go test -v ./...

run: build
	./$(BINARY) --help

.DEFAULT_GOAL := build
