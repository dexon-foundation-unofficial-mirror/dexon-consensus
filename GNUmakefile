# Makefile for DEXON consensus core

DEXON_CONSENSUS_CORE=github.com/dexon-foundation/dexon-consensus-core

.PHONY: clean default

default: all

all: dexcon-simulation

pre-submit: lint

dexcon-simulation:
	go install $(DEXON_CONSENSUS_CORE)/cmd/dexcon-simulation

pre-submit: lint test vet

lint:
	@$(GOPATH)/bin/golint -set_exit_status `go list ./... | grep -v 'vendor'`

vet:
	@go vet `go list ./... | grep -v 'vendor'`

test:
	@for pkg in `go list ./... | grep -v 'vendor'`; do \
		if ! go test $$pkg; then \
			echo 'Some test failed, abort'; \
			exit 1; \
		fi; \
	done
