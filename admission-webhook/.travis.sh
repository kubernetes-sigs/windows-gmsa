#!/usr/bin/env bash

## Runs the right Travis tests depending on the environment variables.
## Must stay in syncs with the build matrix from .travis.yml

set -e

KUBECTL=~/.kubeadm-dind-cluster/kubectl

main() {
    case "$T" in
        unit)
            make unit_tests ;;
        integration)
            run_integration_tests ;;
        dry_run_deploy)
            run_dry_run_deploy ;;
        *)
            echo "Unknown test option: $T" && exit 1 ;;
    esac
}

run_integration_tests() {
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

# performs a dry-run deploy and ensures no changes have been made to the cluster
run_dry_run_deploy() {
    make cluster_start

    wait_for_all_terminating_k8s_resources || return $?

    local SNAPSHOT_DIR='k8s_snapshot'
    k8s_snapshot $SNAPSHOT_DIR/before

    KUBECTL=$KUBECTL ./deploy/deploy-gmsa-webhook.sh --file gmsa-webhook.yml --dry-run

    k8s_snapshot $SNAPSHOT_DIR/after

    diff $SNAPSHOT_DIR/{before,after}
}

# lists all API objects present on a k8s master node and saves them to the folder given as 1st argument
# that dir shouldn't exist prior to calling the function
k8s_snapshot() {
    local DIR="$1"
    [ "$DIR" ] && [ ! -d "$DIR" ] || return 1
    mkdir -p "$DIR"

    local RESOURCE OUTPUT
    for RESOURCE in $($KUBECTL api-resources -o name); do
        OUTPUT="$(list_k8s_resources "$RESOURCE")" || return $?
        echo "$OUTPUT" | sort > "$DIR/$RESOURCE"
    done
}

# lists all API objects of the given resource, with an optional JSON-path filter
list_k8s_resources() {
    local RESOURCE="$1"

    local FILTER
    if [ "$2" ]; then
        FILTER="?(@.$2)"
    else
        FILTER='*'
    fi

    local OUTPUT EXIT_STATUS=0

    # this output is guaranteed to be unique since namespaces can't contain spaces
    OUTPUT="$($KUBECTL get "$RESOURCE" --all-namespaces -o jsonpath="{range .items[$FILTER]}{@.metadata.namespace}{\" \"}{@.metadata.name}{\" \"}{@.status.phase}{\"\n\"}{end}" 2>&1)" \
        || EXIT_STATUS=$?

    if [[ $EXIT_STATUS == 0 ]]; then
        echo "$OUTPUT"
        return 0
    elif [[ "$OUTPUT" == *'(NotFound)'* ]] || [[ "$OUTPUT" == *'(MethodNotAllowed)'* ]]; then
        return 0
    else
        echo "Error when listing k8s resource $RESOURCE: $OUTPUT" 1>&2
        return $EXIT_STATUS
    fi
}

# waits for all API objects in "Terminating" state to go away, for up to
# 60 secs per resource type
wait_for_all_terminating_k8s_resources() {
    local RESOURCE
    for RESOURCE in $($KUBECTL api-resources -o name); do
        wait_for_terminating_k8s_resources "$RESOURCE" || return $?
    done
}

# waits up to 60 seconds for API objects of the given resource that are
# in "Terminating" state to go away, else errors out
wait_for_terminating_k8s_resources() {
    local RESOURCE="$1"

    local START="$(date -u +%s)" OUTPUT

    while [ "$(( $(date -u +%s) - $START ))" -le 60 ]; do
        OUTPUT="$(list_k8s_resources "$RESOURCE" 'status.phase=="Terminating"')" || return $?
        if [ "$OUTPUT" ]; then
            # there still are terminating resources
            sleep 1
            continue
        fi
        return 0
    done

    echo -e "Timed out waiting for all terminating $RESOURCE to go away:\n$OUTPUT"
    return 1
}

main "$@"
