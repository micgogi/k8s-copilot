#!/usr/bin/env bash
# up.sh — boot a local kind cluster + Istio + Bookinfo demo workload.
#
# Usage:
#   ./up.sh              # healthy cluster
#   ./up.sh --with-fault # inject a broken image into reviews-v1 for demo
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-k8s-copilot-demo}"
ISTIO_VERSION="${ISTIO_VERSION:-1.23.0}"
ISTIO_MINOR="${ISTIO_VERSION%.*}"

WITH_FAULT=false
for arg in "$@"; do
  [[ "$arg" == "--with-fault" ]] && WITH_FAULT=true
done

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "✗ required tool not found: $1" >&2
    echo "  install hint: $2" >&2
    exit 1
  fi
}

require docker    "https://docs.docker.com/get-docker/ (or: brew install --cask docker)"
require kind      "brew install kind"
require kubectl   "brew install kubectl"
require istioctl  "brew install istioctl"

if ! kind get clusters | grep -qx "${CLUSTER_NAME}"; then
  echo "→ Creating kind cluster: ${CLUSTER_NAME}"
  kind create cluster --name "${CLUSTER_NAME}"
else
  echo "→ kind cluster ${CLUSTER_NAME} already exists"
fi

kubectl config use-context "kind-${CLUSTER_NAME}" >/dev/null

if ! kubectl get ns istio-system >/dev/null 2>&1; then
  echo "→ Installing Istio (demo profile)"
  istioctl install --set profile=demo -y
else
  echo "→ Istio already installed"
fi

kubectl label namespace default istio-injection=enabled --overwrite >/dev/null

if ! kubectl get deploy productpage-v1 >/dev/null 2>&1; then
  echo "→ Deploying Bookinfo sample app"
  kubectl apply -f "https://raw.githubusercontent.com/istio/istio/release-${ISTIO_MINOR}/samples/bookinfo/platform/kube/bookinfo.yaml"
else
  echo "→ Bookinfo already deployed"
fi

echo "→ Waiting for productpage to be ready (timeout 180s)..."
kubectl wait --for=condition=ready pod -l app=productpage --timeout=180s || true

if $WITH_FAULT; then
  echo "→ Injecting fault: pointing reviews-v1 at a nonexistent image"
  kubectl set image deployment/reviews-v1 \
    reviews=istio/examples-bookinfo-reviews-v1:does-not-exist
fi

echo
kubectl get pods -n default
echo
echo "✓ Demo cluster ready. Try:"
echo "    kcp diagnose -n default"
