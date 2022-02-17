# Release Process

The Kubernetes Windows GMSA project is released on an as-needed basis. The process is as follows:

1. An issue is created proposing a new release with a changelog since the last release
1. All [OWNERS](OWNERS) must LGTM this release issue
1. An OWNER runs `git tag -s $VERSION` from `master` branch and pushes the tag with `git push $VERSION`
1. An OWNER promotes the `gcr.io/k8s-staging-gmsa-webhook/k8s-gmsa-webhook` image built the tagged commit.
    1. Insuctions TBD
1. An OWNER creates a release with by
    1. Navigating to [releases](https://github.com/kubernetes-sigs/windows-gmsa/releases) and clicking on `Draft a new release`
    1. Selecting the tag for the current release version
    1. Setting the title of the release to the current release version
    1. Adding instructions on how to deploy the current release with the following template:

        To deploy:

        ```bash
        K8S_GMSA_DEPLOY_DOWNLOAD_REV='$VERSION' \
            ./deploy-gmsa-webhook.sh --file ./gmsa-manifests \
            --image k8s.gcr.io/sig-windows/k8s-gmsa-webhook:'$VERSION'
        ```

    1. Adding release notes from release issue
    1. Clicking on `Publish Release`
1. The release issue is closed
1. An announcement email is sent to `kubernetes-sig-windows@googlegroups.com` with the subject `[ANNOUNCE] Kubernetes SIG-Windows GMSA Webhook $VERSION is Released`
1. An announcement is posted in `#SIG-windows` in the Kubernetes slack.
