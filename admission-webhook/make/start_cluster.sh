#!/usr/bin/env bash

set -e

usage() {
    cat <<EOF
Light wrapper around kind that generates the right configuration file for kind, then starts the cluster.

usage: $0 --name NAME --num-nodes NUM_NODES --version VERSION [--kind-bin KIND_BIN]

NAME is the kind cluster name.
NUM_NODES is the number of worker nodes.
VERSION is the k8s version to use.
KIND_BIN is the path to the kind executable.
EOF
    exit 1
}

setkubeconfig() {
    mkdir -p ~/.kube
    $KIND_BIN get kubeconfig --name "$NAME" >  ~/.kube/kind-config-$NAME
}

main() {
    local NAME=
    local NUM_NODES=
    local VERSION=
    local KIND_BIN=kind

    # parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --name)
                NAME="$2" ;;
            --num-nodes)
                NUM_NODES="$2" ;;
            --version)
                VERSION="$2" ;;
            --kind-bin)
                KIND_BIN="$2" ;;
            *)
                echo "Unknown option: $1"
                usage ;;
        esac
        shift 2
    done

    [ "$NAME" ] && [ "$NUM_NODES" ] && [ "$VERSION" ] || usage

    if [[ "$(${KIND_BIN} get clusters)" == *"${NAME}"* ]]; then
  	  echo "Dev cluster already running. Skipping cluster creation"
      setkubeconfig
  	  exit 0
  	else
  	  echo "Starting new cluster";
  	fi

    local CONFIG_FILE
    CONFIG_FILE="$(mktemp /tmp/gmsa-webhook-kind-config.XXXXXXX)"

    # generate the config file
    cat <<EOF > "$CONFIG_FILE"
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
kubeadmConfigPatches:
- |
  apiVersion: kubeadm.k8s.io/v1beta2
  kind: ClusterConfiguration
  metadata:
    name: config
  apiServer:
    extraArgs:
      enable-admission-plugins: NodeRestriction,MutatingAdmissionWebhook,ValidatingAdmissionWebhook
EOF
    cat <<EOF >> "$CONFIG_FILE"
nodes:
- role: control-plane
EOF
    for _ in $(seq "$NUM_NODES"); do
        echo -e '- role: worker' >> "$CONFIG_FILE"
    done

    # run kind
    local EXIT_STATUS=0
    $KIND_BIN create cluster --name "$NAME" --config "$CONFIG_FILE" --image "kindest/node:v$VERSION" --wait 240s || EXIT_STATUS=$?
    setkubeconfig

    # clean up the config file
    rm -f "$CONFIG_FILE"

    return $EXIT_STATUS
}

main "$@"
