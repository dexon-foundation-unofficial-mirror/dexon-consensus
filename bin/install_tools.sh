#!/bin/sh

: "${GO:="go"}"

if ! command -v dep >/dev/null 2>&1; then
  eval "${GO} get -u ${GO_BUILD_FLAGS} github.com/golang/dep/cmd/dep"
fi
if ! command -v golint >/dev/null 2>&1; then
  eval "${GO} get -u ${GO_BUILD_FLAGS} golang.org/x/lint/golint"
fi
if ! command -v gosec >/dev/null 2>&1; then
  eval "${GO} get -u ${GO_BUILD_FLAGS} github.com/securego/gosec/cmd/gosec/..."
fi
