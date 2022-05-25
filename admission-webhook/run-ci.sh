#!/usr/bin/env bash

## Runs the right Travis tests depending on the environment variables.
## Must stay in syncs with the build matrix from .travis.yml

set -e

# giving a unique name allows running locally with https://github.com/nektos/act
export CLUSTER_NAME="windows-gmsa-$GITHUB_JOB"
export KUBECTL="$GITHUB_WORKSPACE/admission-webhook/dev/kubectl-$CLUSTER_NAME"
export KUBECONFIG="$GITHUB_WORKSPACE/admission-webhook/dev/kubeconfig-$CLUSTER_NAME"

export K8S_GMSA_CHART="$GITHUB_WORKSPACE/charts/gmsa"

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
    if [ "$WITHOUT_ENVSUBST" ] && [ -x "$(command -v envsubst)" ] && [[ "$GITHUB_ACTIONS" == "true" ]]; then
        echo "Removing envsubst"
        sudo rm -f "$(command -v envsubst)"
    fi

    export DEPLOYMENT_NAME=windows-gmsa-dev
    export NAMESPACE=windows-gmsa-dev

    if [[ "$DEPLOY_METHOD" == 'download' ]]; then
        export K8S_GMSA_DEPLOY_METHOD='download'

        if [ "$GITHUB_HEAD_REF" ]; then
            # GITHUB_HEAD_REF is only set if it's a pull request
            export K8S_GMSA_DEPLOY_DOWNLOAD_REPO="$GITHUB_REPOSITORY"
            export K8S_GMSA_DEPLOY_DOWNLOAD_REV="$GITHUB_SHA"
            echo "Running pull request: $K8S_GMSA_DEPLOY_DOWNLOAD_REPO $K8S_GMSA_DEPLOY_DOWNLOAD_REV"
        else
            # not a pull request
            export K8S_GMSA_DEPLOY_DOWNLOAD_REPO="kubernetes-sigs/windows-gmsa"
            export K8S_GMSA_DEPLOY_DOWNLOAD_REV="$(git rev-parse HEAD)"
            echo "Running: $K8S_GMSA_DEPLOY_DOWNLOAD_REPO $K8S_GMSA_DEPLOY_DOWNLOAD_REV"
        fi
    elif [[ "$DEPLOY_METHOD" == 'chart' ]]; then
       export K8S_GMSA_DEPLOY_METHOD='chart'
       echo "deploy method: $K8S_GMSA_DEPLOY_METHOD"
       if [ "$GITHUB_HEAD_REF" ]; then
           # GITHUB_HEAD_REF is only set if it's a pull request
           # Similar logic goes here, but installs the chart using the repo.
           export K8S_GMSA_DEPLOY_DOWNLOAD_REPO="$GITHUB_REPOSITORY"
           export K8S_GMSA_DEPLOY_DOWNLOAD_REV="$GITHUB_SHA"
           echo "Running pull request: $K8S_GMSA_DEPLOY_DOWNLOAD_REPO $K8S_GMSA_DEPLOY_DOWNLOAD_REV"
       else
           # not a pull request
           # Installs the chart using the local copy.
           export K8S_GMSA_DEPLOY_DOWNLOAD_REPO="kubernetes-sigs/windows-gmsa"
           export K8S_GMSA_DEPLOY_DOWNLOAD_REV="$(git rev-parse HEAD)"
           echo "Running: $K8S_GMSA_DEPLOY_DOWNLOAD_REPO $K8S_GMSA_DEPLOY_DOWNLOAD_REV"
       fi
    fi

    
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
        INFO_OUTPUT="$(docker exec "$CLUSTER_NAME-control-plane" curl -sk https://$SERVICE_IP/info)"

        if [[ "$INFO_OUTPUT" == *"$BOGUS_VERSION"* ]]; then
            echo -e "Output from /info does contain '$BOGUS_VERSION':\n$INFO_OUTPUT"
        else
            echo -e "Expected output from /info to contain '$BOGUS_VERSION', instead got:\n$INFO_OUTPUT"
            exit 1
        fi
    else
        if [[ "$DEPLOY_METHOD" == 'download' ]]; then
            make integration_tests
        fi
        if [[ "$DEPLOY_METHOD" == 'chart' ]]; then
            make integration_tests_chart
        fi
    fi
}

# performs a dry-run deploy and ensures no changes have been made to the cluster
run_dry_run_deploy() {
    make cluster_start

    wait_for_all_terminating_or_pending_k8s_resources || return $?

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
    OUTPUT="$($KUBECTL get "$RESOURCE" --all-namespaces -o jsonpath="{range .items[$FILTER]}{@.metadata.namespace}{\" \"}{@.metadata.name}{\"\n\"}{end}" 2>&1)" \
        || EXIT_STATUS=$?

    if [[ "$OUTPUT" == *'deprecated'* ]]; then
        # component status is deprecated in 1.19 and fails https://github.com/kubernetes-sigs/kind/issues/1998
        return 0
    elif [[ $EXIT_STATUS == 0 ]]; then
        echo "$OUTPUT"
        return 0
    elif [[ "$OUTPUT" == *'(NotFound)'* ]] || [[ "$OUTPUT" == *'(MethodNotAllowed)'* ]]; then
        return 0
    else
        echo "Error when listing k8s resource $RESOURCE: $OUTPUT" 1>&2
        return $EXIT_STATUS
    fi
}

# waits for all API objects in "Terminating" or "Pending" state to go away,
# for up to 120 secs per resource type
wait_for_all_terminating_or_pending_k8s_resources() {
    local RESOURCE
    for RESOURCE in $($KUBECTL api-resources -o name); do
        wait_until_no_k8s_resources_in_state "$RESOURCE" 'Terminating' || return $?
        wait_until_no_k8s_resources_in_state "$RESOURCE" 'Pending' || return $?
    done
}

# waits up to 60 seconds for API objects of the given resource that are
# in the given state to go away, else errors out
wait_until_no_k8s_resources_in_state() {
    local RESOURCE="$1"
    local STATE="$2"

    local START="$(date -u +%s)" OUTPUT

    while [ "$(( $(date -u +%s) - $START ))" -le 60 ]; do
        OUTPUT="$(list_k8s_resources "$RESOURCE" 'status.phase=="'$STATE'"')" || return $?
        if [ "$OUTPUT" ]; then
            # there still are resources in the given state
            echo "still waiting on $RESOURCE $OUTPUT"
            sleep 1
            continue
        fi
        return 0
    done

    echo -e "Timed out waiting for all $STATE $RESOURCE to go away:\n$OUTPUT"
    return 1
}

main "$@"
