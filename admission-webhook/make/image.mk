# must stay consistent with the go version defined in .travis.yml
GO_VERSION = 1.12
VERSION = $(shell git rev-parse HEAD)
DOCKER_BUILD = docker build . --build-arg GO_VERSION=$(GO_VERSION) --build-arg VERSION=$(VERSION)

.PHONY: image_build_dev
image_build_dev:
	$(DOCKER_BUILD) -f dockerfiles/Dockerfile.dev -t $(DEV_IMAGE_NAME)

.PHONY: image_build
image_build:
	$(DOCKER_BUILD) -f dockerfiles/Dockerfile -t $(IMAGE_NAME)

.PHONY: image_push
image_push:
	docker push $(IMAGE_NAME)
