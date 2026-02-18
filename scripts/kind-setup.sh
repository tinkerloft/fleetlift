#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${KIND_CLUSTER_NAME:-fleetlift-test}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "==> Creating kind cluster: ${CLUSTER_NAME}"
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    echo "    Cluster already exists, skipping creation"
else
    kind create cluster --name "${CLUSTER_NAME}"
fi

echo "==> Applying K8s manifests"
kubectl apply -f "${ROOT_DIR}/deploy/k8s/"

echo "==> Building agent image"
docker build -f "${ROOT_DIR}/docker/Dockerfile.agent" -t fleetlift-agent:latest "${ROOT_DIR}"

echo "==> Loading agent image into kind"
kind load docker-image fleetlift-agent:latest --name "${CLUSTER_NAME}"

echo "==> Done! Cluster '${CLUSTER_NAME}' is ready."
echo "    kubectl config use-context kind-${CLUSTER_NAME}"
