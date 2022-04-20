HELM = $(shell which helm 2> /dev/null)
HELM_URL = https://get.helm.sh/helm-v$(HELM_VERSION)-$(UNAME)-amd64.tar.gz

ifeq ($(HELM),)
HELM = $(DEV_DIR)/HELM-$(HELM_VERSION)
endif

.PHONY: install-helm
install-helm:
	curl https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash

.PHONY: helm-chart
helm-chart:
	$(HELM) package ../charts/$(VERSION)/gmsa -d ../charts/$(VERSION)

.PHONY: helm-index
helm-index:
	$(HELM) repo index ../charts

.PHONY: helm-lint
helm-lint:
	$(HELM) lint ../charts/$(VERSION)/gmsa

# deploys the chart to the kind cluster with the release image
.PHONY: deploy_chart
deploy_chart: install-helm
	K8S_GMSA_IMAGE=$(IMAGE_NAME) $(MAKE) _deploy_chart

# removes the chart from the kind cluster
.PHONY: remove_chart
remove_chart:
	KUBECONFIG=$(KUBECONFIG) $(HELM) uninstall $(DEPLOYMENT_NAME)

# deploys the webhook to the kind cluster using helm
# if $K8S_GMSA_DEPLOY_METHOD is set to "download", then it will deploy by downloading
# the deploy script as documented in the README, using $K8S_GMSA_DEPLOY_CHART_REPO and
# $K8S_GMSA_DEPLOY_CHART_VERSION env variables to build the download URL. If VERSION is
# not set then latest is used.
.PHONY: _deploy_chart
_deploy_chart:  _deploy_certmanager
ifeq ($(K8S_GMSA_CHART),)
	@ echo "Cannot call target $@ without setting K8S_GMSA_CHART"
	exit 1
endif
	mkdir -p $(dir $(MANIFESTS_FILE))
	@ echo "installing helm deployment $(DEPLOYMENT_NAME) with chart $(K8S_GMSA_CHART) and image $(IMAGE_REPO):$(VERSION)"
	KUBECONFIG=$(KUBECONFIG) $(HELM) version
	KUBECONFIG=$(KUBECONFIG) $(HELM) install $(DEPLOYMENT_NAME) --set image.repository=$(IMAGE_REPO) --set image.tag=$(VERSION) $(K8S_GMSA_CHART)

.PHONY: _deploy_certmanager
_deploy_certmanager: remove_certmanager
	KUBECONFIG=$(KUBECONFIG) $(KUBECTL) create namespace cert-manager
	KUBECONFIG=$(KUBECONFIG) $(KUBECTL) apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.7.1/cert-manager.yaml

.PHONY: remove_certmanager
remove_certmanager:
	KUBECONFIG=$(KUBECONFIG) $(KUBECTL) delete namespace cert-manager || true
