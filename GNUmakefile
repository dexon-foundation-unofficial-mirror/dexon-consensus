# Makefile for DEXON Consensus Core

GOPATH = $(CURDIR)/../../../../
ifndef BINDIR
BINDIR := $(CURDIR)/build
else
BINDIR := $(abspath $(BINDIR))
endif
PROJECT_ROOT=github.com/dexon-foundation/dexon-consensus-core
BUILDER_REPO = cobinhooddev/ci-base-alpine

ifeq ($(DOCKER),true)
GO_LDFLAGS += -linkmode external -extldflags \"-static\"
endif

V ?= 0
AT_LOCAL_GO    = $(AT_LOCAL_GO_$(V))
AT_LOCAL_GO_0  = @echo "  HOST GO    "$1;
AT_LOCAL_GO_1  =
AT_DOCKER_GO   = $(AT_DOCKER_GO_$(V))
AT_DOCKER_GO_0 = @echo "  DOCKER GO  "$1;
AT_DOCKER_GO_1 =

define BUILD_RULE
$1: pre-build
ifeq ($(DOCKER),true)
	$(AT_DOCKER_GO)docker run --rm \
		-v "$(GOPATH)":/go:z \
		-v $(BINDIR):/artifacts:z \
		-e "GOPATH=/go" \
		-w /go/src/$(PROJECT_ROOT) \
		$(BUILDER_REPO):latest sh -c "\
			go build -o /artifacts/$1 $(PROJECT_ROOT)/cmd/$1"
else
	@mkdir -p $(BINDIR)
	$(AT_LOCAL_GO)go install -ldflags '$(GO_LDFLAGS)' $(PROJECT_ROOT)/cmd/$1
	@install -c $(GOPATH)/bin/$1 $(BINDIR)
endif
endef

COMPONENTS = \
	dexcon-simulation \
	dexcon-simulation-peer-server

.PHONY: clean default

default: all

all: $(COMPONENTS)

$(foreach component, $(COMPONENTS), $(eval $(call BUILD_RULE,$(component))))

pre-build: eth-dep

pre-submit: eth-dep check-format lint test vet

eth-dep:
	@rm -rf vendor/github.com/ethereum/go-ethereum/crypto/secp256k1/libsecp256k1
	@if [ ! -d .dep/libsecp256k1 ]; then \
		git init .dep/libsecp256k1; \
		cd .dep/libsecp256k1; \
		git remote add origin https://github.com/ethereum/go-ethereum.git; \
		git config core.sparsecheckout true; \
		echo "crypto/secp256k1/libsecp256k1/*" >> .git/info/sparse-checkout; \
		cd ../../; \
	fi
	@cd .dep/libsecp256k1; git pull --depth=1 origin master; cd ../../
	@cp -r .dep/libsecp256k1/crypto/secp256k1/libsecp256k1 vendor/github.com/ethereum/go-ethereum/crypto/secp256k1

format:
	@go fmt `go list ./... | grep -v 'vendor'`

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

check-format:
	@if gofmt -l `go list -f '{{.Dir}}' ./...` | grep -q go; then \
		echo 'Error: source code not formatted'; \
		exit 1; \
	fi

.ONESHELL:
test-sim: all
	@rm -rf build/test-sim
	@mkdir build/test-sim
	@cp test_config/test.toml build/test-sim/
	@cd build/test-sim ; ../dexcon-simulation-peer-server -config test.toml >& server.log &
	@cd build/test-sim ; ../dexcon-simulation -config test.toml >& /dev/null
	@if grep "error" build/test-sim/server.log -q -i; then \
		exit 1; \
	fi
