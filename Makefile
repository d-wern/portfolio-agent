GOOS := linux
GOARCH := arm64

.PHONY: all
all: build test

.PHONY: build
build: clean
	@echo "Building..."
	@mkdir -p ./cmd/build
	@GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 go build -o ./cmd/build/bootstrap ./cmd

.PHONY: zip
zip:
	@echo "Zipping..."
	@zip -j ./cmd/build/function.zip ./cmd/build/bootstrap

.PHONY: test
test:
	@go test ./... -cover

.PHONY: clean
clean:
	@rm -rf ./cmd/build
