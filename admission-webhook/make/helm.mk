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
deploy_chart:
	$(MAKE) _deploy_chart

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
_deploy_chart: remove_chart
ifeq ($(K8S_GMSA_CHART),)
	@ echo "Cannot call target $@ without setting K8S_GMSA_CHART"
	exit 1
endif
	mkdir -p $(dir $(MANIFESTS_FILE))
ifeq ($(K8S_GMSA_DEPLOY_METHOD),download)
	@if [ ! "$$K8S_GMSA_DEPLOY_DOWNLOAD_REPO" ]; then K8S_GMSA_DEPLOY_DOWNLOAD_REPO="kubernetes-sigs/windows-gmsa"; fi \
      && if [ ! "$$K8S_GMSA_DEPLOY_DOWNLOAD_REV" ]; then K8S_GMSA_DEPLOY_DOWNLOAD_REV="$$(git rev-parse HEAD)"; fi \
      && CMD="curl -sL 'https://raw.githubusercontent.com/$$K8S_GMSA_DEPLOY_DOWNLOAD_REPO/$$K8S_GMSA_DEPLOY_DOWNLOAD_REV/admission-webhook/deploy/deploy-gmsa-webhook.sh' | K8S_GMSA_DEPLOY_DOWNLOAD_REPO='$$K8S_GMSA_DEPLOY_DOWNLOAD_REPO' K8S_GMSA_DEPLOY_DOWNLOAD_REV='$$K8S_GMSA_DEPLOY_DOWNLOAD_REV' KUBECONFIG=$(KUBECONFIG) KUBECTL=$(KUBECTL) bash -s -- --file '$(MANIFESTS_FILE)' --name '$(DEPLOYMENT_NAME)' --namespace '$(NAMESPACE)' --image '$(K8S_GMSA_IMAGE)' --certs-dir '$(CERTS_DIR)' $(EXTRA_GMSA_DEPLOY_ARGS)" \
      && echo "$$CMD" && eval "$$CMD"
else
    KUBECONFIG=$(KUBECONFIG) $(HELM) install $(DEPLOYMENT_NAME)
endif
