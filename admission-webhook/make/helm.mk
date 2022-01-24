.PHONY: install-helm
install-helm:
	curl https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash

.PHONY: helm-chart
helm-chart:
	helm package ../charts/$(VERSION)/gmsa -d ../charts/$(VERSION)

.PHONY: helm-index
helm-index:
	helm repo index ../charts

.PHONY: helm-lint
helm-lint:
	helm lint ../charts/$(VERSION)/gmsa