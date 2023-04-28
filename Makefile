BIN_DIR := bin

.PHONY: all build install lint clean test vet

all: vet build

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

build: $(BIN_DIR)
	go build -o $(BIN_DIR) ./...

install:
	go install ./...

lint:
	golangci-lint run

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -rf $(BIN_DIR)
	go clean ./...
