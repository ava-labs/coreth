#!/bin/bash
go get -d github.com/Determinant/cedrusdb-go
source "$(go env GOPATH)/pkg/mod/github.com/!determinant/cedrusdb-go@$(go list -m github.com/Determinant/cedrusdb-go | awk '{print $2}')/scripts/env.sh" && go build -o payment ./*.go
