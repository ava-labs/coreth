#!/usr/bin/env bash

git ls-files | grep -v "caminogo/" | grep ".go$" |  xargs gofmt -w