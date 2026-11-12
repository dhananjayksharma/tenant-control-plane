#!/bin/sh
set -eu
go vet ./...
go test ./... -race -count=1
go test -tags integration ./tests -race -count=1 -v
