.PHONY: build build-linux

GIT_REVISION=git rev-parse --short HEAD
VERSION=`git describe --tag --abbrev=0 --exact-match HEAD 2> /dev/null || (echo 'Git tag not found, fallback to commit id' >&2; ${GIT_REVISION})`

default: build

build:
	go build -ldflags "-X github.com/ava-labs/coreth/plugin/evm.Version=${VERSION}-debank" -o  bin/evm "plugin/"*.go

build-linux:
	CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build -ldflags "-X github.com/ava-labs/coreth/plugin/evm.Version=${VERSION}-debank" -o bin/evm "plugin/"*.go
