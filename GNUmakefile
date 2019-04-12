# Makefile for DEXON Consensus Core

# Commands
DOCKER       ?= docker
GO           ?= go
GOFMT        ?= gofmt
GOLINT       ?= $(GOBIN)/golint
GREP         ?= grep
INSTALL      ?= install -c
MKDIR_P      ?= mkdir -p
SHELLCHECK   ?= shellcheck

# Paths
ifndef BUILDDIR
BUILDDIR     := $(CURDIR)/build
else
BUILDDIR     := $(abspath $(BUILDDIR))
endif
FIRST_GOPATH := $(shell $(GO) env GOPATH | cut -d: -f1)
GOBIN        ?= $(FIRST_GOPATH)/bin
BLS_LIB       = vendor/github.com/dexon-foundation/bls/lib/libbls384.a

# Automake-like silent rules
V ?= 0
AT_LOCAL_GO        = $(AT_LOCAL_GO_$(V))
AT_LOCAL_GO_0      = @echo "  HOST GO      "$1;
AT_LOCAL_GO_1      =
AT_DOCKER_GO       = $(AT_DOCKER_GO_$(V))
AT_DOCKER_GO_0     = @echo "  DOCKER GO    "$1;
AT_DOCKER_GO_1     =
AT_RUN             = $(AT_RUN_$(V))
AT_RUN_0           = @echo "  RUN          "$@;
AT_RUN_1           =

# Functions
include functions.mk

# Go build variables
GO_IMPORT_PATH          = github.com/dexon-foundation/dexon-consensus
# Handle -tags safely
GO_TAGS                ?=
GO_TAGS_SAFE            = $(call SAFE_STRING,$(GO_TAGS))
GO_TAGS_FLAGS           = -tags $(GO_TAGS_SAFE)
# Handle -ldflags safely
GO_LDFLAGS              =
GO_LDFLAGS_SAFE         = $(call SAFE_STRING,$(GO_LDFLAGS))
GO_LDFLAGS_FLAGS        = -ldflags $(GO_LDFLAGS_SAFE)
# Handle -timeout
GO_TEST_TIMEOUT        := 33m
# Produce common flags
GO_BUILD_COMMON_FLAGS   = $(GO_TAGS_FLAGS) $(GO_LDFLAGS_FLAGS)
GO_VET_COMMON_FLAGS     = $(GO_TAGS_FLAGS)
GO_TEST_COMMON_FLAGS    = -v -timeout $(GO_TEST_TIMEOUT)
# Produce final flags
GO_BUILD_ALL_FLAGS      = $(GO_BUILD_COMMON_FLAGS) $(GO_BUILD_FLAGS)
GO_LIST_ALL_FLAGS       = $(GO_BUILD_COMMON_FLAGS) $(GO_BUILD_FLAGS)
GO_VET_ALL_FLAGS        = $(GO_VET_COMMON_FLAGS) $(GO_VET_FLAGS)
GO_TEST_ALL_FLAGS       = $(GO_TEST_COMMON_FLAGS) $(GO_BUILD_COMMON_FLAGS) $(GO_BUILD_FLAGS)

# Builder images
BUILDER_IMAGE           = dexonfoundation/dexon-alpine:latest
BLS_IMAGE               = dexonfoundation/bls-go-alpine:latest

ifdef BUILD_IN_DOCKER
GO_TAGS                += static
GO_LDFLAGS             += -linkmode external -extldflags -static
GO_BUILD_COMMON_FLAGS  += -buildmode=pie
endif

define BUILD_RULE
$1: pre-build
ifdef BUILD_IN_DOCKER
	$(AT_DOCKER_GO)$(DOCKER) run --rm \
		-v BLSDATA:/data/bls \
		-v $(FIRST_GOPATH):/go:z \
		-v $(BUILDDIR):/artifacts:z \
		-e GOPATH=/go \
		-w /go/src/$(GO_IMPORT_PATH) \
		$(BUILDER_IMAGE) sh -c $(call SAFE_STRING,\
			mv -f $(BLS_LIB) $(BLS_LIB).bak; \
			cp /data/bls/libbls384.a $(BLS_LIB); \
			go build -o /artifacts/$1 $(GO_BUILD_ALL_FLAGS) \
				$(GO_IMPORT_PATH)/cmd/$1; \
			mv -f $(BLS_LIB).bak $(BLS_LIB))
else
	$(AT_LOCAL_GO)$(GO) build -o $(GOBIN)/$1 $(GO_BUILD_ALL_FLAGS) \
		$(GO_IMPORT_PATH)/cmd/$1
	@$(INSTALL) $(GOBIN)/$1 $(BUILDDIR)
endif
endef

TEST_TARGET := $(GO) list $(GO_LIST_ALL_FLAGS) ./... | $(GREP) -v vendor
ifeq ($(NO_INTEGRATION_TEST), true)
	GO_TEST_TIMEOUT := 25m
	TEST_TARGET := $(TEST_TARGET) | $(GREP) -v integration_test
else ifeq ($(ONLY_INTEGRATION_TEST), true)
	TEST_TARGET := $(TEST_TARGET) | $(GREP) integration_test
endif

ifneq ($(NO_TEST_RACE), true)
	GO_TEST_COMMON_FLAGS += -race
endif

COMPONENTS = \
	dexcon-simulation \
	dexcon-simulation-peer-server

.PHONY: clean default

default: all

all: $(COMPONENTS)
ifdef BUILD_IN_DOCKER
	$(DOCKER) volume rm BLSDATA > /dev/null
endif

$(foreach component, $(COMPONENTS), $(eval $(call BUILD_RULE,$(component))))

pre-build: dep docker-dep
	@$(MKDIR_P) $(BUILDDIR)

pre-submit: dep check-format lint vet shellcheck check-security test

dep:
	bin/install_eth_dep.sh
	bin/install_dkg_dep.sh

docker-dep:
ifdef BUILD_IN_DOCKER
	$(DOCKER) volume create BLSDATA > /dev/null
	$(DOCKER) run --rm -v BLSDATA:/data/bls $(BLS_IMAGE) \
		cp -f /usr/lib/libbls384.a /data/bls/
endif

format:
	$(AT_RUN)$(GO) fmt \
		$$($(GO) list $(GO_LIST_ALL_FLAGS) ./... | $(GREP) -v vendor)

lint:
	$(AT_RUN)$(GOBIN)/golint -set_exit_status \
		$$($(GO) list $(GO_LIST_ALL_FLAGS) ./... | $(GREP) -v vendor)

vet:
	$(AT_RUN)$(GO) vet $(GO_VET_ALL_FLAGS) \
		$$($(GO) list $(GO_LIST_ALL_FLAGS) ./... | $(GREP) -v vendor)

shellcheck:
	$(AT_RUN)$(SHELLCHECK) */*.sh */*/*.sh

check-security:
	@rm -f gosec.log
	$(AT_RUN)$(GOBIN)/gosec -quiet -out gosec.log ./... || true
	@if [ -e gosec.log ]; then \
		cat gosec.log; \
		echo 'Error: security issue found'; \
		exit 1; \
	fi


test-short:
	$(AT_RUN)for pkg in $$($(TEST_TARGET)); do \
		if ! $(GO) test -short $(GO_TEST_ALL_FLAGS) "$$pkg"; then \
			echo 'Some test failed, abort'; \
			exit 1; \
		fi; \
	done

test:
	$(AT_RUN)for pkg in $$($(TEST_TARGET)); do \
		if ! $(GO) test $(GO_TEST_ALL_FLAGS) "$$pkg"; then \
			echo 'Some test failed, abort'; \
			exit 1; \
		fi; \
	done

bench:
	$(AT_RUN)for pkg in $$($(GO) list $(GO_LIST_ALL_FLAGS) ./... | $(GREP) -v vendor); do \
		if ! $(GO) test -bench=. -run=^$$ $(GO_TEST_nALL_FLAGS) "$$pkg"; then \
			echo 'Some test failed, abort'; \
			exit 1; \
		fi; \
	done

check-format:
	$(AT_RUN)if $(GOFMT) -l $$($(GO) list -f '{{.Dir}}' $(GO_LIST_ALL_FLAGS) ./...) | $(GREP) -q go; then \
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
	@if $(GREP) "error" build/test-sim/server.log -q -i; then \
		exit 1; \
	fi
