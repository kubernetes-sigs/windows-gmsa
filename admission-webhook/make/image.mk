# must stay consistent with the go version defined in .travis.yml
GO_VERSION = 1.17
DOCKER_BUILD = docker build . --build-arg GO_VERSION=$(GO_VERSION) --build-arg VERSION=$(TAG)

.PHONY: image_build_dev
image_build_dev:
	$(DOCKER_BUILD) -f dockerfiles/Dockerfile.dev -t $(DEV_IMAGE_NAME)

.PHONY: image_build
image_build:
	$(DOCKER_BUILD)  -f dockerfiles/Dockerfile -t $(WEBHOOK_IMG):$(TAG)
	docker tag $(WEBHOOK_IMG):$(TAG) $(WEBHOOK_IMG):latest

.PHONY: image_push
image_push:
	docker push $(WEBHOOK_IMG):$(TAG)
	docker push $(WEBHOOK_IMG):latest
