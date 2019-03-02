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

usage: $0 --file MANIFESTS_FILE [--name NAME] [--namespace NAMESPACE] [--image IMAGE_NAME] [--certs-dir CERTS_DIR] [--dry-run] [--overwrite]

MANIFESTS_FILE is the path to the file where the k8s manifests will be written
NAME defaults to 'gsma-webhook' and is used in the names of most the k8s resources created.
NAMESPACE is the namespace to deploy to; defaults to 'gmsa-webhook' - will error out if the namespace already exists.
IMAGE_NAME is the name of the Docker image containing the webhook; defaults to 'wk88/k8s-gmsa-webhook:latest' (FIXME: figure out a better way to distribute this image)
CERTS_DIR defaults to 'gmsa-webhook-certs'

If --dry-run is set, the script echoes what command it would perform
without actually affecting the k8s cluster.
If the files this script generates already exist and --overwrite is
not set, it will not regenerate the files.
EOF
    exit 1
}

DEPLOY_DIR="$(dirname "$0")"
TMP_DIR_PREFIX='/tmp/gmsa-webhook-deploy-'

ensure_helper_file_present() {
    local NAME="$1"
    local DIR="$DEPLOY_DIR"

    if [ ! -r "$DIR/$NAME" ]; then
        DIR=$(mktemp -d "${TMP_DIR_PREFIX}XXXXXXX")
        local URL="https://raw.githubusercontent.com/kubernetes-sigs/windows-gmsa/master/admission-webhook/deploy/$NAME"

        if which curl &> /dev/null; then
            curl -sL "$URL" > "$DIR/$NAME"
        else
            wget -O "$DIR/$NAME" "$URL"
        fi
    fi

    echo "$DIR/$NAME"
}

main() {
    local MANIFESTS_FILE=
    local NAME='gmsa-webhook'
    local NAMESPACE='gmsa-webhook'
    local IMAGE_NAME='wk88/k8s-gmsa-webhook:latest'
    local CERTS_DIR='gmsa-webhook-certs'
    local DRY_RUN=false
    local OVERWRITE=false

    # parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --file)
                MANIFESTS_FILE="$2" && shift 2 ;;
            --name)
                NAME="$2" && shift 2 ;;
            --namespace)
                NAMESPACE="$2" && shift 2 ;;
            --image-name)
                IMAGE_NAME="$2" && shift 2 ;;
            --certs-dir)
                CERTS_DIR="$2" && shift 2 ;;
            --dry-run)
                DRY_RUN=true && shift ;;
            --overwrite)
                OVERWRITE=true && shift ;;
            *)
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

    if [ ! -x "$(command -v envsubst)" ]; then
        fatal_error 'envsubst not found'
    fi

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

    TLS_PRIVATE_KEY=$(cat "$SERVER_KEY" | base64 -w 0) \
        TLS_CERTIFICATE="$TLS_CERTIFICATE" \
        CA_BUNDLE="$($KUBECTL get configmap -n kube-system extension-apiserver-authentication -o=jsonpath='{.data.client-ca-file}' | base64 -w 0)" \
        RBAC_ROLE_NAME="$NAMESPACE-$NAME-rbac-role" \
        NAME="$NAME" \
        NAMESPACE="$NAMESPACE" \
        IMAGE_NAME="$IMAGE_NAME" \
        envsubst < "$TEMPLATE_PATH" > "$MANIFESTS_FILE"

    echo_or_run --with-kubectl-dry-run "$KUBECTL apply -f $MANIFESTS_FILE"

    if ! $DRY_RUN; then
        info 'Windows GMSA Admission Webhook successfully deployed!'
        info "You can remove it by running $KUBECTL delete -f $MANIFESTS_FILE"
    fi

    # cleanup
    rm -rf "$TMP_DIR_PREFIX"*
}

main "$@"
