# cert-manager Porkbun webhook

An [ACME DNS01](https://cert-manager.io/docs/configuration/acme/dns01/) webhook
solver for [cert-manager](https://cert-manager.io/) that manages challenge
records through the [Porkbun](https://porkbun.com/) API.

This is a maintained continuation of
[mdonoughe/porkbun-webhook](https://github.com/mdonoughe/porkbun-webhook).

## Installation

The chart is published as an OCI artifact on GitHub Container Registry:
`oci://ghcr.io/alessandrozanatta/charts/cert-manager-porkbun-webhook`. The
chart and the image share one version.

### Helm

<!-- x-release-please-start-version -->
```sh
helm install porkbun-webhook \
  oci://ghcr.io/alessandrozanatta/charts/cert-manager-porkbun-webhook \
  --version 0.1.5 \
  --namespace cert-manager \
  --set groupName=acme.example.com
```
<!-- x-release-please-end -->

### Kustomize

<!-- x-release-please-start-version -->
```yaml
# kustomization.yaml (build with `kustomize build --enable-helm`)
helmCharts:
  - name: cert-manager-porkbun-webhook
    repo: oci://ghcr.io/alessandrozanatta/charts
    version: 0.1.5
    releaseName: porkbun-webhook
    namespace: cert-manager
    valuesInline:
      groupName: acme.example.com
```
<!-- x-release-please-end -->

Argo CD renders this with `kustomize.buildOptions: --enable-helm` set in
`argocd-cm`.

### Values

`groupName` is required: it is the API group the webhook serves, so use a
domain you own. See [`values.yaml`](deploy/cert-manager-porkbun-webhook/values.yaml)
for everything else; the notable ones are:

| Value | Default | Description |
| --- | --- | --- |
| `groupName` | `""` | API group referenced by issuers. Required. |
| `certManager.namespace` | `cert-manager` | Namespace cert-manager runs in. |
| `certManager.serviceAccountName` | `cert-manager` | ServiceAccount allowed to call the webhook. |
| `secretAccess` | `porkbun-key` in `certManager.namespace` | Secrets holding Porkbun credentials the webhook may read, per namespace. |
| `image.tag` / `image.digest` | chart appVersion | Image to run. |
| `replicaCount` | `1` | Replicas; pair with `podDisruptionBudget.enabled`. |
| `logLevel` | `2` | klog verbosity. |

## Usage

1. Enable API access for the domain in the Porkbun dashboard and create an
   [API key](https://porkbun.com/account/api).

2. Store the keys in the Secret named in `secretAccess`. For a ClusterIssuer it
   lives in cert-manager's namespace; for an Issuer, in the Issuer's namespace
   (add that namespace to `secretAccess`).

   ```sh
   kubectl -n cert-manager create secret generic porkbun-key \
     --from-literal=api-key=pk1_... \
     --from-literal=secret-key=sk1_...
   ```

3. Configure an issuer:

   ```yaml
   apiVersion: cert-manager.io/v1
   kind: ClusterIssuer
   metadata:
     name: letsencrypt
   spec:
     acme:
       server: https://acme-v02.api.letsencrypt.org/directory
       email: you@example.com
       privateKeySecretRef:
         name: letsencrypt-key
       solvers:
         - selector:
             dnsZones:
               - example.com
           dns01:
             webhook:
               groupName: acme.example.com
               solverName: porkbun
               config:
                 apiKeySecretRef:
                   name: porkbun-key
                   key: api-key
                 secretKeySecretRef:
                   name: porkbun-key
                   key: secret-key
   ```

## Migrating from mdonoughe/porkbun-webhook

The chart was renamed and its labels changed, so it cannot be upgraded in place:

1. Uninstall the old release (or remove it from Argo CD). Issuers and
   certificates are unaffected; renewals pause until the new release is up.
2. Install this chart with the same `groupName`, so existing issuers keep
   working unchanged.
3. Delete the Role/RoleBinding created from the old `rbac.yaml`: the chart now
   grants secret access itself through `secretAccess`. If your secret lives
   elsewhere than `porkbun-key` in `cert-manager`, set `secretAccess`
   accordingly.

## Development

```sh
make unit        # unit tests
make test        # unit tests + cert-manager's DNS01 conformance suite
make lint        # golangci-lint
make chart-test  # helm lint, kubeconform, helm-unittest
make e2e         # kind cluster with cert-manager and this chart
```

The conformance suite runs against an in-memory fake of the Porkbun API
(`internal/porkbuntest`) and a DNS server backed by it, inside a real
kube-apiserver provisioned by `setup-envtest`, so it needs no Porkbun account.

### Releases

Pull request titles follow [Conventional Commits](https://www.conventionalcommits.org/)
and pull requests are squash-merged. Once CI passes on `main`,
[release-please](https://github.com/googleapis/release-please) opens a release
pull request and the release workflow merges it, tags the release, and publishes
the multi-arch image to `ghcr.io/alessandrozanatta/cert-manager-porkbun-webhook`
and the chart to `oci://ghcr.io/alessandrozanatta/charts`, both with build
provenance attestations.

[Renovate](https://docs.renovatebot.com/) keeps dependencies current and
automerges minor, patch and digest updates once CI is green. Major updates wait
for review.
