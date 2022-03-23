# Release Process

The Kubernetes Windows GMSA project is released on an as-needed basis. The process is as follows:

1. An issue is created proposing a new release with a changelog since the last release
1. All [OWNERS](OWNERS) must LGTM this release issue
1. An OWNER runs `git tag -s $VERSION` from `master` branch and pushes the tag with `git push $VERSION`
1. An OWNER promotes the `gcr.io/k8s-staging-gmsa-webhook/k8s-gmsa-webhook` image built the tagged commit.
    1. Follow setup steps for `kpromo` from [here](https://github.com/kubernetes-sigs/promo-tools/blob/main/docs/promotion-pull-requests.md#preparing-environment) if needed
    1. Manually tag the desired container image in the [staging registry](https://console.cloud.google.com/gcr/images/k8s-staging-gmsa-webhook/GLOBAL) as `$VERSION`
    1. Run `kpromo pr` to open a pull request to have tagged container image promoted from staging to release registries

        ```bash
        kpromo pr --project gmsa-webhook --tag $VERSION --reviewers "@jayunit100 @jsturtevant @marosset" --fork {your github username}
        ```

    1. Review / merge image promotion PR

1. An OWNER creates a release with by
    1. Navigating to [releases](https://github.com/kubernetes-sigs/windows-gmsa/releases) and clicking on `Draft a new release`
    1. Selecting the tag for the current release version
    1. Setting the title of the release to the current release version
    1. Clicking `Auto-generate release notes` button (and editing what was generated as appropriate) 
    1. Adding instructions on how to deploy the current release **to the top of the releaes notes** with the following template:

        To deploy:

        ```bash
        K8S_GMSA_DEPLOY_DOWNLOAD_REV='$VERSION' \
            ./deploy-gmsa-webhook.sh --file ./gmsa-manifests \
            --image k8s.gcr.io/gmsa-webhook/k8s-gmsa-webhook:$VERSION
        ```
        
    1. Clicking on `Publish Release`
1. The release issue is closed
1. An announcement email is sent to `kubernetes-sig-windows@googlegroups.com` with the subject `[ANNOUNCE] Kubernetes SIG-Windows GMSA Webhook $VERSION is Released`
1. An announcement is posted in `#SIG-windows` in the Kubernetes slack.
