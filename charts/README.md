# Install Windows GMSA with Helm 3

## Prerequisites
- [install Helm](https://helm.sh/docs/intro/quickstart/#install-helm)

### Tips


### install a specific version
```console
helm repo add windows-gmsa https://raw.githubusercontent.com/kubernetes-sigs/windows-gmsa/master/charts/repo
helm install windows-gmsa/gmsa --namespace kube-system --version v0.4.2
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

| Parameter                                             | Description                                                       | Default                                               |
|-------------------------------------------------------|-------------------------------------------------------------------|-------------------------------------------------------|
| `certificates.certManager.enabled`                    | enable cert manager integration                                   | `true`                                                |
| `certificates.certManager.version`                    | version of cert manager                                           |                                                       |
| `certificates.caBundle`                               | cert-manager disabled, add self-signed ca.crt in base64 format    |                                                       |
| `certificates.secretName`                             | cert-manager disabled, upload certs data as k8s secretName        | `gmsa-server-cert`                                    |
| `credential.enabled `                                 | enable creation of GMSA Credential                                | `true`                                                |
| `credential.domainJoinConfig.dnsName`                 | DNS Domain Name                                                   |                                                       |
| `credential.domainJoinConfig.dnsTreeName`             | DNS Domain Name Root                                              |                                                       |
| `credential.domainJoinConfig.guid`                    | GUID                                                              |                                                       |
| `credential.domainJoinConfig.machineAccountName`      | username of the GMSA account                                      |                                                       |
| `credential.domainJoinConfig.netBiosName`             | NETBIOS Domain Name                                               |                                                       |
| `credential.domainJoinConfig.sid`                     | SID                                                               |                                                       |
| `image.repository`                                    | image repository                                                  | `k8s.gcr.io/gmsa-webhook/k8s-gmsa-webhook`                    |
| `image.tag`                                           | image tag                                                         | `v0.4.0`                                              |
| `image.imagePullPolicy`                               | image pull policy                                                 | `IfNotPresent`                                        |
| `global.systemDefaultRegistry `                       | container registry                                                |                                                       |
| `tolerations`                                         | tolerations                                                       | []                                                    |

## troubleshooting
- Add `--wait -v=5 --debug` in `helm install` command to get detailed error
- Use `kubectl describe` to acquire more info
