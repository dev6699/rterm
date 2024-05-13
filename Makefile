.PHONY: run
run:
	go run cmd/rterm/main.go

.PHONY: build
build:
	CGO_ENABLED=0 go build -o rterm cmd/rterm/main.go