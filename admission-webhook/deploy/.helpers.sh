## Meant to be sourced by other files in this repo

echo_stderr() {
    local COLOR
    local NO_COLOR='\033[0m'

    case "$1" in
        green)
            COLOR='\033[0;32m' ;;
        yellow)
            COLOR='\033[0;33m' ;;
        red)
            COLOR='\033[0;31m' ;;
        esac
    shift 1

    >&2 printf "${COLOR}$@\n${NO_COLOR}"
}

info() {
    echo_stderr 'green' "*** $@ ***"
}

warn() {
    echo_stderr 'yellow' "WARNING: $@"
}

fatal_error() {
    echo_stderr 'red' "FATAL ERROR: $@"
    exit 1
}

if [ ! -x "$KUBECTL" ]; then
    KUBECTL=$(command -v kubectl)

    if [ ! -x "$KUBECTL" ]; then
        fatal_error 'kubectl not found'
    fi
fi

echo_or_run() {
    local WITH_KUBECTL_DRY_RUN=false
    if [[ "$1" == '--with-kubectl-dry-run' ]]; then
        WITH_KUBECTL_DRY_RUN=true
        shift
    fi

    if $DRY_RUN; then
        echo "$@"
        if $WITH_KUBECTL_DRY_RUN; then
            eval "$@ --dry-run=client >&2"
        fi
    else
        eval "$@"
    fi
}

wait_for() {
    local FUN="$1"
    local ERROR_MSG="$2"
    local MAX_ATTEMPTS="$3"
    [ "$MAX_ATTEMPTS" ] || MAX_ATTEMPTS=30

    local OUTPUT
    for _ in $(seq "$MAX_ATTEMPTS"); do
        if OUTPUT=$($FUN); then
            echo "$OUTPUT"
            return
        fi
        sleep 1
    done

    local MSG="$ERROR_MSG, giving up after $MAX_ATTEMPTS attempts"
    if [ "$OUTPUT" ]; then
        MSG+=" - last attempt's output: $OUTPUT"
    fi
    fatal_error "$MSG"
}

SERVER_KEY="$CERTS_DIR/server-key.pem"
SERVER_CERT="$CERTS_DIR/server-cert.pem"

if [ "$K8S_WINDOWS_GMSA_DEPLOY_DEBUG" ]; then
    set -x
fi
