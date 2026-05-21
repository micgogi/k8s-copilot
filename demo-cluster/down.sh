#!/usr/bin/env bash
# down.sh — tear down the local demo cluster.
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-k8s-copilot-demo}"

if kind get clusters | grep -qx "${CLUSTER_NAME}"; then
  echo "→ Deleting kind cluster: ${CLUSTER_NAME}"
  kind delete cluster --name "${CLUSTER_NAME}"
else
  echo "→ No cluster named ${CLUSTER_NAME}"
fi
