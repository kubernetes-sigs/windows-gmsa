# must stay consistent with the go version defined in .travis.yml
GO_VERSION = 1.22
BUILD_ARGS = --build-arg GO_VERSION=$(GO_VERSION) --build-arg VERSION=$(TAG) 
DOCKER_BUILD = docker build . $(BUILD_ARGS)

AMD64_ARGS = --build-arg GOARCH=amd64 --build-arg ARCH=amd64 --platform=linux/amd64
ARM64_ARGS = --build-arg GOARCH=arm64 --build-arg ARCH=arm64 --platform=linux/arm64
BUILDX_BUILD = docker buildx build . $(BUILD_ARGS) --provenance=false --sbom=false


.PHONY: image_build_dev
image_build_dev:
	$(DOCKER_BUILD) -f dockerfiles/Dockerfile.dev -t $(DEV_IMAGE_NAME)

.PHONY: create_buildx_builder
create_buildx_builder:
	docker buildx create --name img-builder --platform linux/amd64 --use

.PHONY: remove_image_builder
remove_image_builder:
	docker buildx rm img-builder || true

# Builds an amd64 image and loads it into the local image store - used during integration tests
image_build: remove_image_builder create_buildx_builder image_build_int remove_image_builder

.PHONY: image_build_int
image_build_int:
	$(BUILDX_BUILD) --load $(AMD64_ARGS) -f dockerfiles/Dockerfile -t $(WEBHOOK_IMG):$(TAG) -t $(WEBHOOK_IMG):latest

# Builds and pushes a multi-arch image (amd64 and arm64) to a remote registry
image_build_and_push: remove_image_builder create_buildx_builder image_build_and_push_int remove_image_builder

.PHONY: image_build_and_push_int
image_build_and_push_int:
	$(BUILDX_BUILD) --push $(AMD64_ARGS) -f dockerfiles/Dockerfile -t $(WEBHOOK_IMG):$(TAG)-amd64 -t $(WEBHOOK_IMG):latest-amd64
	$(BUILDX_BUILD) --push $(ARM64_ARGS) -f dockerfiles/Dockerfile -t $(WEBHOOK_IMG):$(TAG)-arm64 -t $(WEBHOOK_IMG):latest-arm64
	docker manifest create --amend $(WEBHOOK_IMG):$(TAG) $(WEBHOOK_IMG):$(TAG)-amd64 $(WEBHOOK_IMG):$(TAG)-arm64
	docker manifest push --purge $(WEBHOOK_IMG):$(TAG)
	docker manifest create --amend $(WEBHOOK_IMG):latest $(WEBHOOK_IMG):latest-amd64 $(WEBHOOK_IMG):latest-arm64
	docker manifest push --purge $(WEBHOOK_IMG):latest
