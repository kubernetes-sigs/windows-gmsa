.DEFAULT_GOAL := test
SHELL := /bin/bash

include make/*.mk

# the $GO_TEST_FLAGS env vars can be set to eg run only specific tests
.PHONY: test
test:
	go test -v -count=1 -cover "$$GO_TEST_FLAGS"
