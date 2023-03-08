export GOBIN := $(PWD)/bin
export PATH := $(GOBIN):$(PATH)

.PHONY: run
run:
	go run main.go $(ARGS)

.PHONY: tidy
tidy:
	go mod tidy
