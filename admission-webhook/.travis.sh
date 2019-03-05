#!/usr/bin/env bash

## Runs the right Travis tests depending on the environment variables.
## Must stay in syncs with the build matrix from .travis.yml

set -e

function run_integration_tests() {
    if [[ "$DEPLOY_METHOD" == 'download' ]]; then
        export K8S_GMSA_DEPLOY_METHOD='download'

        if [ "$TRAVIS_COMMIT" ] && [ "$TRAVIS_PULL_REQUEST_SHA" ]; then
            # it's a pull request
            export K8S_GMSA_DEPLOY_DOWNLOAD_REPO="$TRAVIS_PULL_REQUEST_SLUG"
            export K8S_GMSA_DEPLOY_DOWNLOAD_REV="$TRAVIS_PULL_REQUEST_SHA"
        else
            # not a pull request
            export K8S_GMSA_DEPLOY_DOWNLOAD_REV="$(git rev-parse HEAD)"
        fi
    fi

    export DEPLOYMENT_NAME=windows-gmsa-dev
    export NAMESPACE=windows-gmsa-dev

    if [ "$WITH_DEV_IMAGE" ]; then
        make integration_tests_with_dev_image

        # for good measure let's check that one can change and restart the webhook when using the dev image
        local KUBECTL=~/.kubeadm-dind-cluster/kubectl
        local BOGUS_VERSION='cannotbeavalidversion'

        local POD_NAME
        POD_NAME="$($KUBECTL -n "$NAMESPACE" get pod --selector=app=$DEPLOYMENT_NAME -o=jsonpath='{.items[0].metadata.name}')"
        $KUBECTL -n "$NAMESPACE" exec "$POD_NAME" -- go build -ldflags="-X main.version=$BOGUS_VERSION"
        $KUBECTL -n "$NAMESPACE" exec "$POD_NAME" -- service webhook restart

        local SERVICE_IP
        SERVICE_IP="$($KUBECTL -n $NAMESPACE get service $DEPLOYMENT_NAME -o=jsonpath='{.spec.clusterIP}')"

        local INFO_OUTPUT
        INFO_OUTPUT="$(docker exec kube-master curl -sk https://$SERVICE_IP/info)"

        if [[ "$INFO_OUTPUT" == *"$BOGUS_VERSION"* ]]; then
            echo -e "Output from /info does contain '$BOGUS_VERSION':\n$INFO_OUTPUT"
        else
            echo -e "Expected output from /info to contain '$BOGUS_VERSION', instead got:\n$INFO_OUTPUT"
            exit 1
        fi
    else
        make integration_tests
    fi
}

case "$T" in
    unit)
        make unit_tests ;;
    integration)
        run_integration_tests ;;
    *)
        echo "Unknown test option: $T" && exit 1 ;;
esac
