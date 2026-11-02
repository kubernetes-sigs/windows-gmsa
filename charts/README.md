# Install Windows GMSA with Helm 3

## Prerequisites

- [install Helm](https://helm.sh/docs/intro/quickstart/#install-helm)

### Tips

### install a specific version

```console
helm repo add windows-gmsa https://raw.githubusercontent.com/kubernetes-sigs/windows-gmsa/master/charts/repo
helm install windows-gmsa/gmsa --namespace kube-system --version v0.4.4
```

### search for all available chart versions

```console
helm search repo -l gmsa
```

## uninstall Windows GMSA

```console
helm uninstall gmsa -n kube-system
```

## latest chart configuration

The following table lists the configurable parameters of the latest GMSA chart and default values.

| Parameter                                          | Description                                                           | Default                                         |
| -------------------------------------------------- | --------------------------------------------------------------------- | ----------------------------------------------- |
| `certificates.certManager.enabled`                 | enable cert manager integration                                       | `true`                                          |
| `certificates.certManager.version`                 | version of cert manager                                               |                                                 |
| `certificates.caBundle`                            | cert-manager disabled, add self-signed ca.crt in base64 format        |                                                 |
| `certificates.secretName`                          | cert-manager disabled, upload certs data as k8s secretName            | `gmsa-server-cert`                              |
| `certificates.certReload.enabled`                  | enable cert reload on changes                                         | `false`                                         |
| `credential.enabled`                               | enable creation of GMSA Credential                                    | `true`                                          |
| `credential.domainJoinConfig.dnsName`              | DNS Domain Name                                                       |                                                 |
| `credential.domainJoinConfig.dnsTreeName`          | DNS Domain Name Root                                                  |                                                 |
| `credential.domainJoinConfig.guid`                 | GUID                                                                  |                                                 |
| `credential.domainJoinConfig.machineAccountName`   | username of the GMSA account                                          |                                                 |
| `credential.domainJoinConfig.netBiosName`          | NETBIOS Domain Name                                                   |                                                 |
| `credential.domainJoinConfig.sid`                  | SID                                                                   |                                                 |
| `credential.hostAccountConfig.pluginGUID`          | GUID of CCG Plugin                                                    |                                                 |
| `credential.hostAccountConfigg.portableCcgVersion` | Version of CCG Plugin                                                 | `1`                                             |
| `credential.hostAccountConfig.pluginInput`         | Input to CCG Plugin                                                   |                                                 |
| `image.repository`                                 | image repository                                                      | `registry.k8s.io/gmsa-webhook/k8s-gmsa-webhook` |
| `image.tag`                                        | image tag                                                             | `v0.4.0`                                        |
| `image.imagePullPolicy`                            | image pull policy                                                     | `IfNotPresent`                                  |
| `global.systemDefaultRegistry`                     | container registry                                                    |                                                 |
| `tolerations`                                      | tolerations                                                           | []                                              |
| `setPodOs`                                         | Enables setting of `OS` field on Pod for supported K8s versions       | `true`                                          |
| `viewerRole`                                       | Enable aggregation of `gmsacredentialspecs` to the built-in view role | `false`                                         |

## Namespace filtering

Both webhooks use `namespaceSelector.matchLabels` (default `{}`) and
`namespaceSelector.matchExpressions` (default `[]`) as additional filters on
**namespace labels**, not Pod labels. All conditions are ANDed with the built-in
exclusions: the Helm release namespace and namespaces labeled
`windows.k8s.io/disabled: "true"` are always excluded. Empty filters preserve the
existing behavior for all other namespaces.

To include only named namespaces, set the following in your values file:

```yaml
namespaceSelector:
  matchExpressions:
    - key: kubernetes.io/metadata.name
      operator: In
      values: ["app-production", "app-staging"]
```

To include all namespaces except specific ones (and the built-in exclusions):

```yaml
namespaceSelector:
  matchExpressions:
    - key: kubernetes.io/metadata.name
      operator: NotIn
      values: ["sandbox", "monitoring"]
```

To opt in using a custom namespace label:

```yaml
namespaceSelector:
  matchLabels:
    windows.k8s.io/gmsa-enabled: "true"
```

Apply the file with:

```console
helm upgrade --install gmsa ./charts/gmsa --namespace gmsa-system --create-namespace -f values.yaml
```
 Label the desired namespaces with
`kubectl label namespace app-production windows.k8s.io/gmsa-enabled=true`
when using the label-based example.

You can combine label and expression filters. Multiple values within `In` are
alternatives; separate expressions must all match. `NotIn` also matches when
the label is absent. Nonmatching namespaces skip **both GMSA mutation and
validation**, including credential-spec injection and service-account permission
checks. Filtering is performed by the Kubernetes API server before calling the
webhooks; no application configuration is required.

## troubleshooting

- Add `--wait -v=5 --debug` in `helm install` command to get detailed error
- Use `kubectl describe` to acquire more info
