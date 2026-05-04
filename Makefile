GO ?= go
PORT ?= 6733
CSV ?= data/devices.csv

.PHONY: build run test test-race fmt vet tidy clean simulate

build:
	$(GO) build -o bin/server ./cmd/server

run:
	$(GO) run ./cmd/server -port $(PORT) -csv $(CSV)

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

clean:
	rm -f bin/server results.txt

# Run the device simulator against a server already running on $(PORT).
simulate:
	./bin/device-simulator -port $(PORT)
