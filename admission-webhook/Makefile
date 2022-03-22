.DEFAULT_GOAL := test
SHELL := /bin/bash

WEBHOOK_ROOT := $(CURDIR)

REGISTRY ?= sigwindowstools
STAGING_REGISTRY ?= gcr.io/k8s-staging-gmsa-webhook
IMAGE_NAME ?= k8s-gmsa-webhook
TAG ?=  $(shell git describe --tags --always `git rev-parse HEAD`)
WEBHOOK_IMG ?= $(REGISTRY)/$(IMAGE_NAME)

DEV_IMAGE_NAME = k8s-windows-gmsa-webhook-dev

CURL = $(shell which curl 2> /dev/null)
WGET = $(shell which wget 2> /dev/null)

ifeq ($(CURL)$(WGET),)
$(error "Neither curl nor wget available")
endif

UNAME = $(shell uname | tr A-Z a-z)
ifeq ($(UNAME),)
$(error "Unable to determine OS type")
endif

include make/*.mk

.PHONY: test
test: deps_install unit_tests integration_tests

# the UNIT_TEST_FLAGS env var can be set to eg run only specific tests, e.g:
# UNIT_TEST_FLAGS='-test.run TestHTTPWebhook' make unit_tests
.PHONY: unit_tests
unit_tests:
	go test -v -count=1 -cover $(UNIT_TEST_FLAGS)

.PHONY: integration_tests
integration_tests: image_build deploy_webhook run_integration_tests

.PHONY: integration_tests_chart
integration_tests_chart: image_build deploy_chart run_integration_tests

.PHONY: integration_tests_with_dev_image
integration_tests_with_dev_image: image_build_dev deploy_dev_webhook run_integration_tests

# the INTEGRATION_TEST_FLAGS env var can be set to eg run only specific tests, e.g.:
# INTEGRATION_TEST_FLAGS='-test.run TestHappyPathWithPodLevelCredSpec' make run_integration_tests
.PHONY: run_integration_tests
run_integration_tests:
	@ echo "### Starting integration tests with Kubernetes version: $(KUBERNETES_VERSION) ###"
	cd integration_tests && KUBECONFIG=$(KUBECONFIG) KUBECTL=$(KUBECTL) go test -count 1 -v $(INTEGRATION_TEST_FLAGS)

.PHONY: clean_integration_tests
clean_integration_tests:
	rm -rf integration_tests/tmp

.PHONY: clean
clean: cluster_clean clean_integration_tests deps_clean

.PHONY: release-staging 
release-staging: ## Builds and push webhook image to k8s-staging bucket
	REGISTRY=$(STAGING_REGISTRY) $(MAKE) image_build image_push
