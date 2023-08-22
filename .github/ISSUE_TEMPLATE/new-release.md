---
name: Cut a release
about: Create a tracking issue for a release cut
title: Cut v0.x.y release
labels: sig/windows
---

## Release Checklist

The release process is documented [HERE](../../RELEASE.md)!

- [ ] Create a new draft [release](https://github.com/kubernetes-sigs/windows-gmsa/releases/new)
  - [ ] Create a new tag targeting `master`
  - [ ] Generate release notes
  - [ ] Set as pre-release (until images are promoted and helm charts updated)
  - [ ] Publish the pre-release

- [ ] Promote the `admission-webhook` image
  - [ ] Manually tag desired container image in the [staging registry](https://console.cloud.google.com/gcr/images/k8s-staging-gmsa-webhook/GLOBAL)
  - [ ] Use `kpromo` to open a image promo PR

        ```bash
        export GITHUB_TOKEN=<your github token>
        kpromo pr --project gmsa-webhook --tag $VERSION --reviewers "@jayunit100 @jsturtevant @marosset" --fork {your github username}

        ```
  - [ ] Verify the image is available using `docker pull registry.k8s.io/gmsa-webhook/k8s-gmsa-webhook:$VERSION`

- [ ] Update helm charts to use new image
  - [ ] Update [Chart.yaml](../../charts/gmsa/Chart.yaml)
    - [ ] Update `appVersion` to match the latest published container image
    - [ ] Bump the `version` as appropriate
  - [ ] Update [values.yaml](../../charts/gmsa/values.yaml)
    - Update `image.tag` to match the latest published container image
  - [ ] Build a helm package

        ```bash
        helm package charts/gmsa --destination ./charts/repo
        ```
  - [ ] Update the repo index

        ```bash
        helm repo index charts/repo/
        ```

- [ ] Update the **IMAGE_NAME** variable in `admission_webhook/deploy/gmsa-webhook.sh` to use the latest image

- [ ] Update the release notes by adding the following template **to the top of the release notes**:

    To deploy:

    ```bash
    K8S_GMSA_DEPLOY_DOWNLOAD_REV='$VERSION' \
        ./deploy-gmsa-webhook.sh --file ./gmsa-manifests \
        --image registry.k8s.io/gmsa-webhook/k8s-gmsa-webhook:$VERSION
    ```

- [ ] Update the release
  - [ ] Unset `Set as pre-release`
  - [ ] Set `Set as latest release`
  - [ ] Update the release

- [ ] Send an announce email to `kuberntes-sig-windows@googlegroups.com` with the subject `[ANNOUNCE] Kubernetes SIG-Windows GMSA Webhook $VERSION is Released`

- [ ] Post new release in slack