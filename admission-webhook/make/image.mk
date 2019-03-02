GO_VERSION = 1.11.5
DOCKER_BUILD = docker build . --build-arg GO_VERSION=$(GO_VERSION)

DEV_IMAGE_NAME = k8s-windows-gmsa-webhook-dev
# FIXME: find a better way to distribute/publish this image
IMAGE_NAME = wk88/k8s-gmsa-webhook:latest

.PHONY: image_build_dev
image_build_dev:
	$(DOCKER_BUILD) -f dockerfiles/Dockerfile.dev -t $(DEV_IMAGE_NAME)

.PHONY: image_build
image_build:
	$(DOCKER_BUILD) -f dockerfiles/Dockerfile -t $(IMAGE_NAME)

.PHONY: image_push
image_push:
	docker push $(IMAGE_NAME)
