# path to glide, will be downloaded if needed
GLIDE_BIN ?= $(shell which glide 2> /dev/null)
ifeq ($(GLIDE_BIN),)
GLIDE_BIN = $(GOPATH)/bin/glide
endif

.PHONY: deps_install
deps_install: $(GLIDE_BIN)
	$(GLIDE_BIN) install -v

.PHONY: deps_update
deps_update: $(GLIDE_BIN)
	$(GLIDE_BIN) update -v

GLIDE_URL = https://glide.sh/get
$(GLIDE_BIN):
ifeq ($(GOPATH),)
	@ echo "GOPATH env var not defined, cannot install glide"
	exit 1
endif
ifeq ($(WGET),)
	$(CURL) $(GLIDE_URL) | sh
else
	$(WGET) -O - $(GLIDE_URL) 2> /dev/null | sh
endif
