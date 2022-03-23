#!/usr/bin/env bash

## Deploys the GMSA webhook

set -e

usage() {
    cat <<EOF
Deploys the GMSA webhook.

Should be run with a kube admin config file present at either the canonical location
(~/.kube/config) or at the path specified by the KUBECONFIG environment variable.

This script:
 * generates a SSL certificate signed by k8s, for mutual authentication
   between the API server and the webhook service
 * deploys a k8s service running the webhook
 * registers that service as a webhook admission controller

usage: $0 --file MANIFESTS_FILE [--name NAME] [--namespace NAMESPACE] [--image IMAGE_NAME] [--certs-dir CERTS_DIR] [--dry-run] [--overwrite] [--tolerate-master]

MANIFESTS_FILE is the path to the file the k8s manifests will be written to.
NAME defaults to 'gmsa-webhook' and is used in the names of most of the k8s resources created.
NAMESPACE is the namespace to deploy to; defaults to 'gmsa-webhook'.
IMAGE_NAME is the name of the Docker image containing the webhook; defaults to 'sigwindowstools/k8s-gmsa-webhook:latest'
CERTS_DIR defaults to 'gmsa-webhook-certs'

If --dry-run is set, the script echoes what command it would perform
without actually affecting the k8s cluster.
If the files this script generates already exist and --overwrite is
not set, it will not regenerate the files.
If --tolerate-master is set, the webhook will tolerate running on master nodes.
EOF
    exit 1
}

DEPLOY_DIR="$(dirname "$0")"
TMP_DIR_PREFIX='/tmp/gmsa-webhook-deploy-'

# it's possible to override these 2 to download from another repo/branch
[ "$K8S_GMSA_DEPLOY_DOWNLOAD_REPO" ] || K8S_GMSA_DEPLOY_DOWNLOAD_REPO='kubernetes-sigs/windows-gmsa'
[ "$K8S_GMSA_DEPLOY_DOWNLOAD_REV" ] || K8S_GMSA_DEPLOY_DOWNLOAD_REV='master'

ensure_helper_file_present() {
    local NAME="$1"
    local DIR="$DEPLOY_DIR"

    if [ ! -r "$DIR/$NAME" ]; then
        DIR=$(mktemp -d "${TMP_DIR_PREFIX}XXXXXXX")
        local URL="https://raw.githubusercontent.com/$K8S_GMSA_DEPLOY_DOWNLOAD_REPO/$K8S_GMSA_DEPLOY_DOWNLOAD_REV/admission-webhook/deploy/$NAME"

        if which curl &> /dev/null; then
            curl -sL "$URL" > "$DIR/$NAME"
        else
            wget -O "$DIR/$NAME" "$URL"
        fi
    fi

    realpath "$DIR/$NAME"
}

write_manifests_file() {
    local TEMPLATE_PATH="$1"
    local MANIFESTS_FILE="$2"

    if [ -x "$(command -v envsubst)" ] && [ ! "$WITHOUT_ENVSUBST" ]; then
        echo "using local envsubst"
        envsubst < "$TEMPLATE_PATH" > "$MANIFESTS_FILE"
    elif [ -x "$(command -v docker)" ]; then
        echo "using envsubst in docker"
        # due to a bug in --env-file convert varaibles we care about to -e parameters 
        # The sed commands get only the env names before =, clean any white space, add -e to them, then make it all one line
        # https://github.com/moby/moby/issues/12997#issuecomment-307665540
        ENVS=`env | grep -E 'NAME|NAMESPACE|TLS|RBAC|TOLERATIONS|IMAGE|CA' | sed -n '/^[^\t]/s/=.*//p' | sed '/^$/d' | sed 's/^/-e /g' | tr '\n' ' '`

        # envsubst is installed in the nginx images which we already maintain
        docker run --rm -v "$TEMPLATE_PATH:$TEMPLATE_PATH" $ENVS k8s.gcr.io/e2e-test-images/nginx:1.15-1 sh -c "cat $TEMPLATE_PATH | envsubst" > $MANIFESTS_FILE
    else
        fatal_error "Unable to run envsubst"
    fi
}

main() {
    local MANIFESTS_FILE=
    local NAME='gmsa-webhook'
    local NAMESPACE='gmsa-webhook'
    local IMAGE_NAME='sigwindowstools/k8s-gmsa-webhook:latest'
    local CERTS_DIR='gmsa-webhook-certs'
    local DRY_RUN=false
    local OVERWRITE=false
    local TOLERATE_MASTER=false

    # parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --file)
                MANIFESTS_FILE="$2" && shift 2 ;;
            --name)
                NAME="$2" && shift 2 ;;
            --namespace)
                NAMESPACE="$2" && shift 2 ;;
            --image)
                IMAGE_NAME="$2" && shift 2 ;;
            --certs-dir)
                CERTS_DIR="$2" && shift 2 ;;
            --dry-run)
                DRY_RUN=true && shift ;;
            --overwrite)
                OVERWRITE=true && shift ;;
            --tolerate-master)
                TOLERATE_MASTER=true && shift ;;
            *)
                echo "Unknown option: $1"
                usage ;;
        esac
    done

    [ "$MANIFESTS_FILE" ] || usage

    # download the helper scripts if needed
    local HELPERS_SCRIPT
    HELPER_SCRIPT=$(ensure_helper_file_present '.helpers.sh')
    . "$HELPER_SCRIPT"
    local CREATE_SIGNED_CERT_SCRIPT
    CREATE_SIGNED_CERT_SCRIPT=$(ensure_helper_file_present 'create-signed-cert.sh')

    if [ -d "$CERTS_DIR" ]; then
        $OVERWRITE || warn "Certs dir $CERTS_DIR already exists"
    else
        mkdir -p "$CERTS_DIR"
    fi

    # create the SSL cert and apply it to the cluster
    local CREATE_CERT_CMD="$BASH $CREATE_SIGNED_CERT_SCRIPT --service $NAME --namespace $NAMESPACE --certs-dir $CERTS_DIR"
    $DRY_RUN && CREATE_CERT_CMD+=" --dry-run" || true
    $OVERWRITE && CREATE_CERT_CMD+=" --overwrite" || true
    eval "K8S_WINDOWS_GMSA_HELPER_SCRIPT='$HELPER_SCRIPT' $CREATE_CERT_CMD"

    # create the CRD
    local CRD_MANIFEST_PATH=$(ensure_helper_file_present 'gmsa-crd.yml')
    local CRD_MANIFEST_CONTENTS=$(cat "$CRD_MANIFEST_PATH")
    if ! $DRY_RUN && $KUBECTL get crd gmsacredentialspecs.windows.k8s.io &> /dev/null; then
        $KUBECTL delete crd gmsacredentialspecs.windows.k8s.io
    fi
    echo_or_run --with-kubectl-dry-run "$KUBECTL create -f - <<< '$CRD_MANIFEST_CONTENTS'"

    # then render the template for the rest of the resources
    local TEMPLATE_PATH
    TEMPLATE_PATH=$(ensure_helper_file_present 'gmsa-webhook.yml.tpl')

    # the TLS certificate might not have been generated yet if it's a dry run
    local TLS_CERTIFICATE
    if [ -r "$SERVER_CERT" ]; then
        TLS_CERTIFICATE=$(cat "$SERVER_CERT" | base64 -w 0)
    elif $DRY_RUN; then
        TLS_CERTIFICATE='TBD'
    else
        fatal_error "Expected to find the server certificate at $SERVER_CERT"
    fi

    TOLERATIONS=''
    if $TOLERATE_MASTER; then
        TOLERATIONS='
      tolerations:
      - key: node-role.kubernetes.io/master
        operator: Exists
        effect: NoSchedule
      - key: node-role.kubernetes.io/control-plane
        operator: Exists
        effect: NoSchedule'
    fi

    if [ -f "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt" ]; then
        info 'using pod based authentication'
        BUNDLE=$(cat /var/run/secrets/kubernetes.io/serviceaccount/ca.crt | base64 | tr -d '\n')
    else
        info 'using config file authentication'
        BUNDLE=$($KUBECTL config view --raw --minify --flatten -o jsonpath='{.clusters[].cluster.certificate-authority-data}')
    fi

    if [[ -z "$BUNDLE" ]]; then
        fatal_error "Not able to determine CA bundle for depoloyment"
    fi

    TLS_PRIVATE_KEY=$(cat "$SERVER_KEY" | base64 -w 0) \
        TLS_CERTIFICATE="$TLS_CERTIFICATE" \
        CA_BUNDLE="$BUNDLE" \
        RBAC_ROLE_NAME="$NAMESPACE-$NAME-rbac-role" \
        NAME="$NAME" \
        NAMESPACE="$NAMESPACE" \
        IMAGE_NAME="$IMAGE_NAME" \
        TOLERATIONS="$TOLERATIONS" \
        write_manifests_file "$TEMPLATE_PATH" "$MANIFESTS_FILE"

    echo_or_run --with-kubectl-dry-run "$KUBECTL apply -f $MANIFESTS_FILE"

    if ! $DRY_RUN; then
        verify_webhook_ready() {
            local READY
            if READY="$($KUBECTL -n "$NAMESPACE" get pod --selector=app=$NAME -o=jsonpath='{.items[0].status.containerStatuses[0].ready}' 2> /dev/null)"; then
                [[ "$READY" == 'true' ]]
            else
                return 1
            fi
        }
        wait_for verify_webhook_ready 'webhook not ready'

        info 'Windows GMSA Admission Webhook successfully deployed!'
        info "You can remove it by running $KUBECTL delete -f $MANIFESTS_FILE"
    fi

    # cleanup
    rm -rf "$TMP_DIR_PREFIX"*
}

main "$@"
