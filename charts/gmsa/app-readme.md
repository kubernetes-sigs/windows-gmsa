# Windows GMSA Admission Webhook

This chart creates the GMSA CRD, Credential, and Admission Webhook. The official documentation and tutorials can be found [here](https://github.com/kubernetes-sigs/windows-gmsa).

## Prerequisites

- Active Directory that supports Group Managed Service Accounts
- A Group Managed Service Account
- Kubernetes v1.23+

## Namespace filtering

Use `namespaceSelector.matchLabels` and `namespaceSelector.matchExpressions` in
values.yaml to select namespaces for both webhooks. These filters are ANDed with
the built-in exclusions for the release namespace and namespaces labeled
`windows.k8s.io/disabled: "true"`. Empty filters preserve the default behavior.
Excluded namespaces skip both GMSA mutation and validation. See the
[Helm documentation](../README.md#namespace-filtering) for examples.
