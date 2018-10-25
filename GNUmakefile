# Makefile for DEXON Consensus Core

GOPATH = $(CURDIR)/../../../../
ifndef BINDIR
BINDIR := $(CURDIR)/build
else
BINDIR := $(abspath $(BINDIR))
endif
PROJECT_ROOT=github.com/dexon-foundation/dexon-consensus-core
BLS_REPO = spiderpowa/bls-go-alpine
BLS_LIB = vendor/github.com/Spiderpowa/bls/lib/libbls384.a
BUILDER_REPO = dexonfoundation/dexon-alpine

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
		-v BLSDATA:/data/bls \
		-v "$(GOPATH)":/go:z \
		-v $(BINDIR):/artifacts:z \
		-e "GOPATH=/go" \
		-w /go/src/$(PROJECT_ROOT) \
		$(BUILDER_REPO):latest sh -c "\
			mv -f $(BLS_LIB) $(BLS_LIB).bak; \
			cp /data/bls/libbls384.a $(BLS_LIB) ;\
			go build -o /artifacts/$1 $(PROJECT_ROOT)/cmd/$1; \
			mv -f $(BLS_LIB).bak $(BLS_LIB)"
else
	@mkdir -p $(BINDIR)
	$(AT_LOCAL_GO)go install -ldflags '$(GO_LDFLAGS)' $(PROJECT_ROOT)/cmd/$1
	@install -c $(GOPATH)/bin/$1 $(BINDIR)
endif
endef

COMPONENTS = \
	dexcon-simulation \
	dexcon-simulation-peer-server \
	dexcon-simulation-with-scheduler

.PHONY: clean default

default: all

all: $(COMPONENTS)
ifeq ($(DOCKER),true)
	@docker volume rm BLSDATA > /dev/null
endif

$(foreach component, $(COMPONENTS), $(eval $(call BUILD_RULE,$(component))))

pre-build: dep docker-dep

pre-submit: dep check-format lint test vet

dep:
	@bin/install_eth_dep.sh
	@bin/install_dkg_dep.sh

docker-dep:
ifeq ($(DOCKER),true)
	@docker run --rm -v BLSDATA:/data/bls $(BLS_REPO):latest \
	sh -c "cp -f /usr/lib/libbls384.a /data/bls/"
endif

format:
	@go fmt `go list ./... | grep -v 'vendor'`

lint:
	@$(GOPATH)/bin/golint -set_exit_status `go list ./... | grep -v 'vendor'`

vet:
	@go vet `go list ./... | grep -v 'vendor'`

test-short:
	@for pkg in `go list ./... | grep -v 'vendor'`; do \
		if ! go test -race -short $$pkg; then \
			echo 'Some test failed, abort'; \
			exit 1; \
		fi; \
	done

test:
	@for pkg in `go list ./... | grep -v 'vendor'`; do \
		if ! go test -timeout 15m -race $$pkg; then \
			echo 'Some test failed, abort'; \
			exit 1; \
		fi; \
	done

bench:
	@for pkg in `go list ./... | grep -v 'vendor'`; do \
		if ! go test -bench=. -run=^$$ $$pkg; then \
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
