# Windows GMSA Webhook Admission controller

## How to deploy

Assuming that `kubectl` is in your path and that your cluster's kube admin config file is present at either the canonical location
(`~/.kube/config`) or at the path specified by the `KUBECONFIG` environment variable, simply run:
```bash
curl -sL https://raw.githubusercontent.com/kubernetes-sigs/windows-gmsa/master/admission-webhook/deploy/deploy-gmsa-webhook.sh | bash -s -- --file webhook-manifests.yml
```

Run with the `--dry-run` option to not change anything to your cluster just yet and simply review the change it would be doing.

Run with `--help` to see all the available options.
