# K8S version can be overriden
# see available versions at https://hub.docker.com/r/kindest/node/tags
KUBERNETES_VERSION ?= 1.23.4
# see https://github.com/kubernetes-sigs/kind/releases
KIND_VERSION = 0.12.0
# https://github.com/helm/helm/releases
HELM_VERSION ?= 3.8.0

CLUSTER_NAME ?= windows-gmsa-dev
DEPLOYMENT_NAME ?= windows-gmsa-dev
NAMESPACE ?= windows-gmsa-dev
NUM_NODES ?= 1

DEV_DIR = $(WEBHOOK_ROOT)/dev

CERTS_DIR = $(DEV_DIR)/certs_dir
MANIFESTS_FILE = $(DEV_DIR)/gmsa-webhook.yml

KIND = $(DEV_DIR)/kind-$(KIND_VERSION)
KIND_URL = https://github.com/kubernetes-sigs/kind/releases/download/v$(KIND_VERSION)/kind-$(UNAME)-amd64

KUBECTL = $(shell which kubectl 2> /dev/null)
KUBECTL_URL = https://storage.googleapis.com/kubernetes-release/release/v$(KUBERNETES_VERSION)/bin/$(UNAME)/amd64/kubectl

ifeq ($(KUBECTL),)
KUBECTL = $(DEV_DIR)/kubectl-$(KUBERNETES_VERSION)
endif

KUBECONFIG?="~/.kube/kind-config-$(CLUSTER_NAME)"

# starts a new kind cluster (see https://github.com/kubernetes-sigs/kind)
.PHONY: cluster_start
cluster_start: $(KIND) $(KUBECTL)
	./make/start_cluster.sh --name '$(CLUSTER_NAME)' --num-nodes $(NUM_NODES) --version $(KUBERNETES_VERSION) --kind-bin "$(KIND)"
	@ echo '### Kubectl version: ###'
	KUBECONFIG=$(KUBECONFIG) $(KUBECTL) version
	# coredns has a thing for creating API resources continually, which confuses the dry-run test
	# since it's not needed for anything here, there's no reason to keep it around
	KUBECONFIG=$(KUBECONFIG) $(KUBECTL) delete -n kube-system deployment.apps/coredns || true
	# kind removes the taint on master when NUM_NODES is 0 - but we do want to test that case too!
	KUBECONFIG=$(KUBECONFIG) $(KUBECTL) taint node $(CLUSTER_NAME)-control-plane 'node-role.kubernetes.io/master=true:NoSchedule' --overwrite
	#@ echo -e 'Cluster started, KUBECONFIG available at $(KUBECONFIG), eg\nexport KUBECONFIG=$(KUBECONFIG)'
	#@ $(MAKE) cluster_symlinks

# removes the kind cluster
.PHONY: cluster_clean
cluster_clean: $(KIND) clean_certs
	$(KIND) delete cluster --name '$(CLUSTER_NAME)'
	# also clean up any left over ci clusters from act
	$(KIND) delete cluster --name 'windows-gmsa-deploy-method-download'
	$(KIND) delete cluster --name 'windows-gmsa-dry-run-deploy'
	$(KIND) delete cluster --name 'windows-gmsa-integration'
	$(KIND) delete cluster --name 'windows-gmsa-tolerate-control-plane'

.PHONY: clean_certs
clean_certs:
	rm -rf $(CERTS_DIR)

# deploys the webhook to the kind cluster with the dev image
.PHONY: deploy_dev_webhook
deploy_dev_webhook:
	K8S_GMSA_IMAGE=$(DEV_IMAGE_NAME) $(MAKE) _deploy_webhook

# deploys the webhook to the kind cluster with the release image
.PHONY: deploy_webhook
deploy_webhook:
	K8S_GMSA_IMAGE=$(WEBHOOK_IMG) $(MAKE) _deploy_webhook

# removes the webhook from the kind cluster
.PHONY: remove_webhook
remove_webhook:
ifeq ($(wildcard $(MANIFESTS_FILE)),)
	@ echo "No manifests file found at $(MANIFESTS_FILE), nothing to remove"
else
	KUBECONFIG=$(KUBECONFIG) $(KUBECTL) delete -f $(MANIFESTS_FILE) || true
endif

# cluster_symlinks symlinks kubectl to dev/kubectl, and the kube config to dev/kubeconfig (used in ci)
.PHONY:
cluster_symlinks:
	ln -sfv $(KUBECTL) $(DEV_DIR)/kubectl-$(CLUSTER_NAME)
	ln -sfv $(KUBECONFIG) $(DEV_DIR)/kubeconfig-$(CLUSTER_NAME)

### "Private" targets below ###

# starts the kind cluster only if it's not already running
.PHONY: _start_cluster_if_not_running
_start_cluster_if_not_running: $(KUBECTL) $(KIND)
	$(MAKE) cluster_start

# deploys the webhook to the kind cluster
# if $K8S_GMSA_DEPLOY_METHOD is set to "download", then it will deploy by downloading
# the deploy script as documented in the README, using $K8S_GMSA_DEPLOY_DOWNLOAD_REPO and
# $K8S_GMSA_DEPLOY_DOWNLOAD_REV env variables to build the download URL. If REV is not set the current
# HEAD's SHA is used.
.PHONY: _deploy_webhook
_deploy_webhook: _copy_image remove_webhook
ifeq ($(K8S_GMSA_IMAGE),)
	@ echo "Cannot call target $@ without setting K8S_GMSA_IMAGE"
	exit 1
endif
	mkdir -p $(dir $(MANIFESTS_FILE))
ifeq ($(K8S_GMSA_DEPLOY_METHOD),download)
	@if [ ! "$$K8S_GMSA_DEPLOY_DOWNLOAD_REPO" ]; then K8S_GMSA_DEPLOY_DOWNLOAD_REPO="kubernetes-sigs/windows-gmsa"; fi \
      && if [ ! "$$K8S_GMSA_DEPLOY_DOWNLOAD_REV" ]; then K8S_GMSA_DEPLOY_DOWNLOAD_REV="$$(git rev-parse HEAD)"; fi \
      && CMD="curl -sL 'https://raw.githubusercontent.com/$$K8S_GMSA_DEPLOY_DOWNLOAD_REPO/$$K8S_GMSA_DEPLOY_DOWNLOAD_REV/admission-webhook/deploy/deploy-gmsa-webhook.sh' | K8S_GMSA_DEPLOY_DOWNLOAD_REPO='$$K8S_GMSA_DEPLOY_DOWNLOAD_REPO' K8S_GMSA_DEPLOY_DOWNLOAD_REV='$$K8S_GMSA_DEPLOY_DOWNLOAD_REV' KUBECONFIG=$(KUBECONFIG) KUBECTL=$(KUBECTL) bash -s -- --file '$(MANIFESTS_FILE)' --name '$(DEPLOYMENT_NAME)' --namespace '$(NAMESPACE)' --image '$(K8S_GMSA_IMAGE)' --certs-dir '$(CERTS_DIR)' $(EXTRA_GMSA_DEPLOY_ARGS)" \
      && echo "$$CMD" && eval "$$CMD"
else
	KUBECONFIG=$(KUBECONFIG) KUBECTL=$(KUBECTL) ./deploy/deploy-gmsa-webhook.sh --file "$(MANIFESTS_FILE)" --name "$(DEPLOYMENT_NAME)" --namespace "$(NAMESPACE)" --image "$(K8S_GMSA_IMAGE)" --certs-dir "$(CERTS_DIR)" $(EXTRA_GMSA_DEPLOY_ARGS)
endif

# copies the image to the kind cluster
.PHONY: _copy_image
_copy_image: _start_cluster_if_not_running
ifeq ($(K8S_GMSA_IMAGE),)
	@ echo "Cannot call target $@ without setting K8S_GMSA_IMAGE"
	exit 1
endif
	$(KIND) load docker-image $(K8S_GMSA_IMAGE) --name '$(CLUSTER_NAME)'

$(KIND):
	mkdir -vp "$$(dirname $(KIND))"
ifeq ($(WGET),)
	$(CURL) -L $(KIND_URL) > $(KIND)
else
	$(WGET) -O $(KIND) $(KIND_URL)
endif
	chmod +x $(KIND)

$(KUBECTL):
	mkdir -vp "$$(dirname $(KUBECTL))"
ifeq ($(WGET),)
	$(CURL) -L $(KUBECTL_URL) > $(KUBECTL)
else
	$(WGET) -O $(KUBECTL) $(KUBECTL_URL)
endif
	chmod +x $(KUBECTL)
