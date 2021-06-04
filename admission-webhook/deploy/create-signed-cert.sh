#!/usr/bin/env bash

## Generates cluster-valid SSL certs for the webhook service
## Inspired from
## https://raw.githubusercontent.com/istio/istio/release-0.7/install/kubernetes/webhook-create-signed-cert.sh
## whose license is also Apache 2.0

set -e

usage() {
    cat <<EOF
Generates certificate suitable for use with the GMSA webhook service.

This script uses k8s' CertificateSigningRequest API to a generate a
certificate signed by k8s CA suitable for use with the GMSA webhook
service. This requires permissions to create and approve CSR. See
https://kubernetes.io/docs/tasks/tls/managing-tls-in-a-cluster for
detailed explantion and additional instructions.

usage: $0 --service SERVICE_NAME --namespace NAMESPACE_NAME --certs-dir PATH/TO/CERTS/DIR [--dry-run] [--overwrite]

If --dry-run is set, the script echoes what command it would perform
to stdout without actually affecting the k8s cluster.
If the files this script generates already exist and --overwrite is
not set, it will not regenerate the files.
EOF
    exit 1
}

SERVICE=
NAMESPACE=
CERTS_DIR=
DRY_RUN=false
OVERWRITE=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --service)
            SERVICE="$2" && shift 2 ;;
        --namespace)
            NAMESPACE="$2" && shift 2 ;;
        --certs-dir)
            CERTS_DIR="$2" && shift 2 ;;
        --dry-run)
            DRY_RUN=true && shift ;;
        --overwrite)
            OVERWRITE=true && shift ;;
        *)
            echo "Unknown option: $1"
            usage ;;
    esac
done

[ "$SERVICE" ] && [ "$NAMESPACE" ] && [ "$CERTS_DIR" ] || usage

if [ ! "$K8S_WINDOWS_GMSA_HELPER_SCRIPT" ]; then
    DEPLOY_DIR="$(dirname "$0")"
    K8S_WINDOWS_GMSA_HELPER_SCRIPT="$DEPLOY_DIR/.helpers.sh"
fi
. "$K8S_WINDOWS_GMSA_HELPER_SCRIPT"

if [ ! -x "$(command -v openssl)" ]; then
    fatal_error 'openssl not found'
fi

gen_file() {
    local FUN="$1"
    local FILE_PATH="$2"

    if [ -f "$FILE_PATH" ] && ! $OVERWRITE; then
        warn "$FILE_PATH already exists, not re-generating"
    else
        $FUN
    fi
}

gen_server_key() { openssl genrsa -out "$SERVER_KEY" 2048; }
gen_file gen_server_key "$SERVER_KEY"

CSR_CONF="$CERTS_DIR/csr.conf"
gen_csr_conf() {
    cat <<EOF >> "$CSR_CONF"
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names
[alt_names]
DNS.1 = $SERVICE
DNS.2 = $SERVICE.$NAMESPACE
DNS.3 = $SERVICE.$NAMESPACE.svc
EOF
}
gen_file gen_csr_conf "$CSR_CONF"

SERVER_CSR="$CERTS_DIR/server.csr"
gen_server_scr() { openssl req -new -key "$SERVER_KEY" -subj "/O=system:nodes/CN=system:node:$SERVICE.$NAMESPACE.svc" -out "$SERVER_CSR" -config "$CSR_CONF"; }
gen_file gen_server_scr "$SERVER_CSR"

CSR_NAME="$SERVICE.$NAMESPACE"
# clean-up any previously created CSR for our service
if ! $DRY_RUN && $KUBECTL get csr "$CSR_NAME" &> /dev/null; then
    $KUBECTL delete csr "$CSR_NAME"
fi

# create server cert/key CSR and send to k8s API
CSR_CONTENTS=$(cat <<EOF
apiVersion: certificates.k8s.io/v1
kind: CertificateSigningRequest
metadata:
  name: $CSR_NAME
spec:
  groups:
  - system:authenticated
  request: $(cat "$SERVER_CSR" | base64 -w 0)
  signerName: kubernetes.io/kubelet-serving
  usages:
  - digital signature
  - key encipherment
  - server auth
EOF
)
echo_or_run --with-kubectl-dry-run "$KUBECTL create -f - <<< '$CSR_CONTENTS'"

if ! $DRY_RUN; then
    verify_csr_created() { $KUBECTL get csr "$CSR_NAME" 2>&1 ; }
    wait_for verify_csr_created "CSR $CSR_NAME not properly created"
fi

# approve and fetch the signed certificate
echo_or_run "$KUBECTL certificate approve $CSR_NAME"

if ! $DRY_RUN; then
    verify_cert_signed() {
        local CERT_CONTENTS
        CERT_CONTENTS=$($KUBECTL get csr $CSR_NAME -o jsonpath='{.status.certificate}')
        echo "$CERT_CONTENTS"
        [[ "$CERT_CONTENTS" != "" ]]
    }
    SERVER_CERT_CONTENTS=$(wait_for verify_cert_signed "after approving CSR $CSR_NAME, the signed certificate did not appear on the resource")

    gen_server_cert() { echo "$SERVER_CERT_CONTENTS" | openssl base64 -d -A -out "$SERVER_CERT"; }
    gen_file gen_server_cert "$SERVER_CERT"
fi
