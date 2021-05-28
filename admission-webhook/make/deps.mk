.PHONY: deps_install
deps_install:
	go mod vendor

.PHONY: deps_update
deps_update:
	go mod tidy

.PHONY: deps_clean
deps_clean:
	rm -rf ./vendor