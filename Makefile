MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rules

.PHONY: all
all: vet test build

.PHONY: build
build:
	go build ./cmd/ipni

.PHONY: install
install:
	go install ./cmd/ipni

.PHONY: lint
lint:
	golangci-lint run

.PHONY: test
test:
	go test ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: clean
clean:
	rm -f ipni
	go clean
