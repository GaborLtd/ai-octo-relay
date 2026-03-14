APP := ai-octo-relay
BIN := ./bin/$(APP)
CONFIG ?= ./config.json

.PHONY: build restart

build:
	mkdir -p ./bin
	go build -o $(BIN) ./cmd/ai-octo-relay

restart: build
	$(BIN) restart -c $(CONFIG)
