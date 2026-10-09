Manifests the kubernetes service applies into every new tenant cluster before a Node joins
(deploy/kubernetes/README.md). Files are applied in name order; `__NODERA_API_HOST__` and
`__NODERA_API_PORT__` are replaced with the tenant's Endpoint. `10-cilium.yaml` is rendered from
Cilium's Helm chart with the command in its header and holds no Secret.
