SHELL := /bin/bash
VERSION ?= 0.1.0-dev
OUT ?= $(CURDIR)/src/bin

.PHONY: all web build test vet clean

all: build

web:
	cd src/web && npm run build

build:
	VERSION="$(VERSION)" OUT="$(OUT)" ./scripts/build.sh

test:
	cd src && go test ./...

vet:
	cd src && go vet ./...

clean:
	rm -rf src/bin
	find src/internal/webembed/dist -mindepth 1 ! -name .keep -exec rm -rf {} +
