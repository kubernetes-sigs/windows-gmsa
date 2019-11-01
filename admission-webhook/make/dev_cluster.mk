# K8S version can be overriden
# see available versions at https://hub.docker.com/r/kindest/node/tags
KUBERNETES_VERSION ?= 1.16.1
# see https://github.com/kubernetes-sigs/kind/releases
KIND_VERSION = 0.5.1

ifeq ($(filter $(KUBERNETES_VERSION),1.16.1),)
$(error "Kubernetes version $(KUBERNETES_VERSION) not supported")
endif

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

KUBECONFIG = ~/.kube/kind-config-$(CLUSTER_NAME)

# starts a new kind cluster (see https://github.com/kubernetes-sigs/kind)
.PHONY: cluster_start
cluster_start: $(KIND) $(KUBECTL)
	./make/start_cluster.sh --name '$(CLUSTER_NAME)' --num-nodes $(NUM_NODES) --version $(KUBERNETES_VERSION) --kind-bin "$(KIND)"
	@ echo '### Kubectl version: ###'
	KUBECONFIG=$(KUBECONFIG) $(KUBECTL) version
	# coredns has a thing for creating API resources continually, which confuses the dry-run test
	# since it's not needed for anything here, there's no reason to keep it around
	KUBECONFIG=$(KUBECONFIG) $(KUBECTL) delete -n kube-system deployment.apps/coredns
	# kind removes the taint on master when NUM_NODES is 0 - but we do want to test that case too!
	KUBECONFIG=$(KUBECONFIG) $(KUBECTL) taint node $(CLUSTER_NAME)-control-plane 'node-role.kubernetes.io/master=true:NoSchedule' --overwrite
	@ echo -e 'Cluster started, KUBECONFIG available at $(KUBECONFIG), eg\nexport KUBECONFIG=$(KUBECONFIG)'
	@ $(MAKE) cluster_symlinks

# removes the kind cluster
.PHONY: cluster_clean
cluster_clean: $(KIND) clean_certs
	$(KIND) delete cluster --name '$(CLUSTER_NAME)'

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
	K8S_GMSA_IMAGE=$(IMAGE_NAME) $(MAKE) _deploy_webhook

# removes the webhook from the kind cluster
.PHONY: remove_webhook
remove_webhook:
ifeq ($(wildcard $(MANIFESTS_FILE)),)
	@ echo "No manifests file found at $(MANIFESTS_FILE), nothing to remove"
else
	KUBECONFIG=$(KUBECONFIG) $(KUBECTL) delete -f $(MANIFESTS_FILE) || true
endif

# cluster_symlinks symlinks kubectl to dev/kubectl, and the kube config to dev/kubeconfig
.PHONY:
cluster_symlinks:
	ln -sfv $(KUBECTL) $(DEV_DIR)/kubectl
	ln -sfv $(KUBECONFIG) $(DEV_DIR)/kubeconfig

### "Private" targets below ###

# starts the kind cluster only if it's not already running
.PHONY: _start_cluster_if_not_running
_start_cluster_if_not_running: $(KUBECTL) $(KIND)
	@ if KUBECONFIG=$(KUBECONFIG) timeout 2 $(KUBECTL) version &> /dev/null; then \
		echo "Dev cluster already running"; \
	else \
		$(MAKE) cluster_start; \
	fi

# deploys the webhook to the kind cluster
# if $K8S_GMSA_DEPLOY_METHOD is set to "download", then it will deploy by downloading
# the deploy script as documented in the README, using $K8S_GMSA_DEPLOY_DOWNLOAD_REPO and
# $K8S_GMSA_DEPLOY_DOWNLOAD_REV env variables to build the download URL. If those two are not
# set, it will try to infer them from the current's branch remote branch and the current
# HEAD's SHA.
.PHONY: _deploy_webhook
_deploy_webhook: _copy_image remove_webhook
ifeq ($(K8S_GMSA_IMAGE),)
	@ echo "Cannot call target $@ without setting K8S_GMSA_IMAGE"
	exit 1
endif
	mkdir -p $(dir $(MANIFESTS_FILE))
ifeq ($(K8S_GMSA_DEPLOY_METHOD),download)
	@ if [ ! "$$K8S_GMSA_DEPLOY_DOWNLOAD_REPO" ]; then \
        SHORT_UPSTREAM="$$(git for-each-ref --format='%(upstream:short)' "$$(git symbolic-ref -q HEAD)" 2>/dev/null)"; \
        if [[ $$? == 0 ]]; then \
          REMOTE_NAME=$${SHORT_UPSTREAM%%/*} ; \
            REMOTE_URL="$$(git remote get-url "$$REMOTE_NAME" 2>/dev/null)"; \
            if [[ $$? == 0 ]]; then \
              REPO_OWNER_AND_NAME=$${REMOTE_URL#*:} && K8S_GMSA_DEPLOY_DOWNLOAD_REPO=$${REPO_OWNER_AND_NAME%.*}; \
            fi; \
        fi; \
        [ "$$K8S_GMSA_DEPLOY_DOWNLOAD_REPO" ] || K8S_GMSA_DEPLOY_DOWNLOAD_REPO='kubernetes-sigs/windows-gmsa'; \
      fi \
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
