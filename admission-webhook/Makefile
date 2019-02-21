.DEFAULT_GOAL := test
SHELL := /bin/bash

# path to glide, will be downloaded if needed
GLIDE_BIN ?= $(shell which glide 2> /dev/null)
ifeq ($(GLIDE_BIN),)
GLIDE_BIN = $(GOPATH)/bin/glide
endif

.PHONY: test
test:
	go test -v -count=1 -cover "$$GO_TEST_FLAGS"

.PHONY: install_deps
install_deps: $(GLIDE_BIN)
	$(GLIDE_BIN) install -v

.PHONY: update_deps
update_deps: $(GLIDE_BIN)
	$(GLIDE_BIN) update -v

GLIDE_URL = https://glide.sh/get
$(GLIDE_BIN):
	@ if [ ! "$$GOPATH" ]; then \
		echo "GOPATH env var not defined, cannot install glide"; \
		exit 1; \
	fi
	mkdir -p $(dir $(GLIDE_BIN))
	if which curl &> /dev/null; then \
		curl $(GLIDE_URL) | sh; \
	else \
		wget -O - $(GLIDE_URL) 2> /dev/null | sh; \
	fi
