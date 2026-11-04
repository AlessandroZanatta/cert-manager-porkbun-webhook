#!/usr/bin/env bash
# End-to-end smoke test: deploys cert-manager and this chart into a kind
# cluster with a freshly built image, then sends the webhook a ChallengePayload
# through the Kubernetes API aggregation layer, as cert-manager does.
#
# The request is set up to fail right after reading the credential secret, so
# it exercises APIService registration, CA injection, authentication,
# authorization, request decoding, solver routing and secret RBAC without
# contacting Porkbun.
set -euo pipefail

# renovate: datasource=github-releases depName=cert-manager/cert-manager
CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-v1.21.3}"
CLUSTER="${KIND_CLUSTER:-porkbun-e2e}"
IMAGE="ghcr.io/alessandrozanatta/cert-manager-porkbun-webhook:e2e"
GROUP="acme.e2e.test"
NS="cert-manager"
RELEASE="porkbun"
KEEP_CLUSTER="${KEEP_CLUSTER:-}"

cd "$(dirname "$0")/.."

log() { printf '\n==> %s\n' "$*"; }

dump() {
  log "Diagnostics"
  kubectl get pods,apiservices -A -o wide || true
  kubectl -n "$NS" describe deploy "$RELEASE-cert-manager-porkbun-webhook" || true
  kubectl -n "$NS" logs "deploy/$RELEASE-cert-manager-porkbun-webhook" --tail=200 || true
}

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then dump; fi
  if [ -z "$KEEP_CLUSTER" ]; then kind delete cluster --name "$CLUSTER" || true; fi
  exit "$status"
}

if ! kind get clusters | grep -qx "$CLUSTER"; then
  log "Creating kind cluster $CLUSTER"
  kind create cluster --name "$CLUSTER" --wait 2m
fi
trap cleanup EXIT
kubectl config use-context "kind-$CLUSTER"

log "Building $IMAGE"
docker build -t "$IMAGE" .
kind load docker-image "$IMAGE" --name "$CLUSTER"

log "Installing cert-manager $CERT_MANAGER_VERSION"
helm upgrade --install cert-manager oci://quay.io/jetstack/charts/cert-manager \
  --version "$CERT_MANAGER_VERSION" --namespace "$NS" --create-namespace \
  --set crds.enabled=true --wait --timeout 5m

log "Installing the webhook chart"
helm upgrade --install "$RELEASE" deploy/cert-manager-porkbun-webhook \
  --namespace "$NS" \
  --set groupName="$GROUP" \
  --set image.tag=e2e --set image.pullPolicy=Never \
  --wait --timeout 3m

log "Waiting for the APIService"
kubectl wait --for=condition=Available "apiservice/v1alpha1.$GROUP" --timeout=3m

log "Creating a credential secret without its secret-key entry"
kubectl -n "$NS" create secret generic porkbun-key --from-literal=api-key=pk1_e2e \
  --dry-run=client -o yaml | kubectl apply -f -

payload=$(cat <<JSON
{
  "apiVersion": "webhook.acme.cert-manager.io/v1alpha1",
  "kind": "ChallengePayload",
  "request": {
    "uid": "e2e",
    "action": "Present",
    "type": "dns-01",
    "dnsName": "example.com",
    "key": "e2e-key",
    "resourceNamespace": "$NS",
    "resolvedFQDN": "_acme-challenge.example.com.",
    "resolvedZone": "example.com.",
    "allowAmbientCredentials": false,
    "config": {
      "apiKeySecretRef": {"name": "porkbun-key", "key": "api-key"},
      "secretKeySecretRef": {"name": "porkbun-key", "key": "secret-key"}
    }
  }
}
JSON
)

log "Sending a ChallengePayload as cert-manager"
response=$(printf '%s' "$payload" | kubectl create --raw "/apis/$GROUP/v1alpha1/porkbun" \
  --as="system:serviceaccount:$NS:cert-manager" -f -)
echo "$response"

want="secretKeySecretRef: secret $NS/porkbun-key has no key \\\"secret-key\\\""
if ! grep -qF "$want" <<<"$response"; then
  echo "unexpected response, want message containing: $want" >&2
  exit 1
fi

log "Checking the webhook logged no permission errors"
if kubectl -n "$NS" logs "deploy/$RELEASE-cert-manager-porkbun-webhook" | grep -i "forbidden"; then
  echo "webhook logged authorization errors" >&2
  exit 1
fi

log "E2E passed"
