# Windows GMSA Webhook Admission controller

## Supported versions

This branch supports versions 1.23 and later. 

## How to deploy

Assuming that `kubectl` is in your path and that your cluster's kube admin config file is present at either the canonical location
(`~/.kube/config`) or at the path specified by the `KUBECONFIG` environment variable, simply run:
```bash
curl -sL https://raw.githubusercontent.com/kubernetes-sigs/windows-gmsa/master/admission-webhook/deploy/deploy-gmsa-webhook.sh | bash -s -- --file webhook-manifests.yml
```

Run with the `--dry-run` option to not change anything to your cluster just yet and simply review the change it would be doing.

Run with `--help` to see all the available options.

## Amazon EKS

According to the Amazon EKS certificate signing documentation, all clusters running Amazon EKS version 1.22 or newer supports the following signer beta.eks.amazonaws.com/app-serving for Kubernetes Certificate Signing Requests (CSR) which is compatible with the latest gMSA admission webhook installation. As a result, we need to replace kubernetes.io/kubelet-serving signer in the gMSA Webhook create-signed-cert.sh file with the following signer : beta.eks.amazonaws.com/app-serving
