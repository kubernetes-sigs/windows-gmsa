# K8S version can be overrident
KUBERNETES_VERSION ?= 1.13
# see https://github.com/kubernetes-sigs/kubeadm-dind-cluster/releases
KUBEADM_DIND_VERSION = v0.1.0

ifeq ($(filter $(KUBERNETES_VERSION),1.11 1.12 1.13),)
$(error "Kubernetes version $(KUBERNETES_VERSION) not supported")
endif

DEPLOYMENT_NAME ?= windows-gmsa-dev
NAMESPACE ?= windows-gmsa-dev

# kubeadm DinD settings
KUBEADM_DIND_CLUSTER_SCRIPT = dev/kubeadm_dind_scripts/$(KUBEADM_DIND_VERSION)/dind-cluster-v$(KUBERNETES_VERSION).sh
KUBEADM_DIND_CLUSTER_SCRIPT_URL = https://github.com/kubernetes-sigs/kubeadm-dind-cluster/releases/download/$(KUBEADM_DIND_VERSION)/dind-cluster-v$(KUBERNETES_VERSION).sh
KUBEADM_DIND_DIR = ~/.kubeadm-dind-cluster
ADMISSION_PLUGINS = NodeRestriction,MutatingAdmissionWebhook,ValidatingAdmissionWebhook

KUBECTL = $(KUBEADM_DIND_DIR)/kubectl
CERTS_DIR = dev/certs_dir
MANIFESTS_FILE = dev/gmsa-webhook.yml

# starts a new DinD cluster (see https://github.com/kubernetes-sigs/kubeadm-dind-cluster)
.PHONY: cluster_start
cluster_start: $(KUBEADM_DIND_CLUSTER_SCRIPT)
	NUM_NODES=1 APISERVER_enable_admission_plugins=$(ADMISSION_PLUGINS) $(KUBEADM_DIND_CLUSTER_SCRIPT) up
	@ echo "### Kubectl version: ###"
	$(KUBECTL) version

# stops the DinD cluster
.PHONY: cluster_stop
cluster_stop: $(KUBEADM_DIND_CLUSTER_SCRIPT)
	$(KUBEADM_DIND_CLUSTER_SCRIPT) down

# removes the DinD cluster
.PHONY: clean_cluster
clean_cluster: clean_certs cluster_stop
	$(KUBEADM_DIND_CLUSTER_SCRIPT) clean
	rm -rf $(KUBEADM_DIND_DIR)

.PHONY: clean_certs
clean_certs:
	rm -rf $(CERTS_DIR)

# deploys the webhook to the DinD cluster with the dev image
.PHONY: deploy_dev_webhook
deploy_dev_webhook:
	K8S_GMSA_IMAGE=$(DEV_IMAGE_NAME) $(MAKE) _deploy_webhook

# deploys the webhook to the DinD cluster with the release image
.PHONY: deploy_webhook
deploy_webhook:
	K8S_GMSA_IMAGE=$(IMAGE_NAME) $(MAKE) _deploy_webhook

# removes the webhook from the DinD cluster
.PHONY: remove_webhook
remove_webhook:
ifeq ($(wildcard $(MANIFESTS_FILE)),)
	@ echo "No manifests file found at $(MANIFESTS_FILE), nothing to remove"
else
	$(KUBECTL) delete -f $(MANIFESTS_FILE) || true
endif

### "Private" targets below ###

# starts the DinD cluster only if it's not already running
.PHONY: _start_cluster_if_not_running
_start_cluster_if_not_running: $(KUBEADM_DIND_CLUSTER_SCRIPT)
	@ if [ -x $(KUBECTL) ] && timeout 2 $(KUBECTL) version &> /dev/null; then \
		echo "Dev cluster already running"; \
	else \
		$(MAKE) cluster_start; \
	fi

# deploys the webhook to the DinD cluster
# if $K8S_GMSA_DEPLOY_METHOD is set to "download", then it will deploy by downloading
# the deploy script as documented in the README, using $K8S_GMSA_DEPLOY_DOWNLOAD_REPO and
# $K8S_GMSA_DEPLOY_DOWNLOAD_REV env variables to build the download URL. If those two are not
# set, it will try to infer them from the current's branch remote branch and the current
# HEAD's SHA.
.PHONY: _deploy_webhook
_deploy_webhook: _copy_image_if_needed remove_webhook
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
      && CMD="curl -sL 'https://raw.githubusercontent.com/$$K8S_GMSA_DEPLOY_DOWNLOAD_REPO/$$K8S_GMSA_DEPLOY_DOWNLOAD_REV/admission-webhook/deploy/deploy-gmsa-webhook.sh' | K8S_GMSA_DEPLOY_DOWNLOAD_REPO='$$K8S_GMSA_DEPLOY_DOWNLOAD_REPO' K8S_GMSA_DEPLOY_DOWNLOAD_REV='$$K8S_GMSA_DEPLOY_DOWNLOAD_REV' KUBECTL=$(KUBECTL) bash -s -- --file '$(MANIFESTS_FILE)' --name '$(DEPLOYMENT_NAME)' --namespace '$(NAMESPACE)' --image '$(K8S_GMSA_IMAGE)' --certs-dir '$(CERTS_DIR)'" \
      && echo "$$CMD" && eval "$$CMD"
else
	KUBECTL=$(KUBECTL) ./deploy/deploy-gmsa-webhook.sh --file "$(MANIFESTS_FILE)" --name "$(DEPLOYMENT_NAME)" --namespace "$(NAMESPACE)" --image "$(K8S_GMSA_IMAGE)" --certs-dir "$(CERTS_DIR)"
endif

# copies the image to the DinD cluster only if it's not already up-to-date
.PHONY: _copy_image_if_needed
_copy_image_if_needed: _start_cluster_if_not_running
ifeq ($(K8S_GMSA_IMAGE),)
	@ echo "Cannot call target $@ without setting K8S_GMSA_IMAGE"
	exit 1
endif
	@ LOCAL_IMG_ID=$$(docker image inspect "$$K8S_GMSA_IMAGE" -f '{{ .Id }}'); \
	  STATUS=$$? ; if [[ $$STATUS != 0 ]]; then echo "Unable to retrieve image ID for $$K8S_GMSA_IMAGE"; exit $$STATUS; fi; \
	  REMOTE_IMG_ID=$$(docker exec kube-master docker image inspect "$$K8S_GMSA_IMAGE" -f '{{ .Id }}' 2> /dev/null); \
	  if [[ $$? == 0 ]] && [[ "$$REMOTE_IMG_ID" == "$$LOCAL_IMG_ID" ]]; then \
	    echo "Image $$K8S_GMSA_IMAGE already up-to-date in DIND cluster"; \
	  else \
		  echo "Copying image $$K8S_GMSA_IMAGE to DIND cluster..." \
		    && CMD="$(KUBEADM_DIND_CLUSTER_SCRIPT) copy-image $$K8S_GMSA_IMAGE" \
		    && echo "$$CMD" && eval "$$CMD"; \
	  fi

$(KUBEADM_DIND_CLUSTER_SCRIPT):
	mkdir -p $(dir $(KUBEADM_DIND_CLUSTER_SCRIPT))
ifeq ($(WGET),)
	$(CURL) -L $(KUBEADM_DIND_CLUSTER_SCRIPT_URL) > $(KUBEADM_DIND_CLUSTER_SCRIPT)
else
	$(WGET) -O $(KUBEADM_DIND_CLUSTER_SCRIPT) $(KUBEADM_DIND_CLUSTER_SCRIPT_URL)
endif
	chmod +x $(KUBEADM_DIND_CLUSTER_SCRIPT)
