.DEFAULT_GOAL := test
SHELL := /bin/bash

DEV_IMAGE_NAME = k8s-windows-gmsa-webhook-dev
# FIXME: find a better way to distribute/publish this image
IMAGE_NAME = wk88/k8s-gmsa-webhook:latest

CURL = $(shell which curl 2> /dev/null)
WGET = $(shell which wget 2> /dev/null)

ifeq ($(CURL)$(WGET),)
$(error "Neither curl nor wget available")
endif

include make/*.mk

.PHONY: test
test: unit_tests integration_tests

# the UNIT_TEST_FLAGS env var can be set to eg run only specific tests
.PHONY: unit_tests
unit_tests:
	go test -v -count=1 -cover "$$UNIT_TEST_FLAGS"

.PHONY: integration_tests
integration_tests: image_build deploy_webhook run_integration_tests

.PHONY: integration_tests_with_dev_image
integration_tests_with_dev_image: image_build_dev deploy_dev_webhook run_integration_tests

# the INTEGRATION_TEST_FLAGS env var can be set to eg run only specific tests
.PHONY: run_integration_tests
run_integration_tests:
	@ echo "### Starting integration tests with Kubernetes version: $(KUBERNETES_VERSION) ###"
	cd integration_tests && KUBECTL=$(KUBECTL) go test -count 1 -v "$$INTEGRATION_TEST_FLAGS"

.PHONY: clean_integration_tests
clean_integration_tests:
	rm -rf integration_tests/tmp

.PHONY: clean
clean: clean_cluster clean_integration_tests
