#!/bin/bash
# cloud-init user-data: install k3s in server mode.
# Root SSH is set up by the workflow after the instance is reachable.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
apt-get update -q
apt-get install -y -q curl

curl -sfL https://get.k3s.io | \
  INSTALL_K3S_EXEC="--disable=traefik --disable=servicelb --write-kubeconfig-mode=644" \
  sh -

# Wait for k3s to be ready (up to 3 minutes).
timeout 180 bash -c 'until k3s kubectl get nodes 2>/dev/null | grep -q " Ready"; do sleep 3; done'

echo "k3s installation complete: $(k3s --version | head -1)"
