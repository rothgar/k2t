#!/bin/bash
# cloud-init user-data: install k3s in server mode.
set -euo pipefail

# Set up SSH key for the ubuntu user immediately so the runner can connect
# while the rest of the installation proceeds.
mkdir -p /home/ubuntu/.ssh
echo "__SSH_PUBLIC_KEY__" >> /home/ubuntu/.ssh/authorized_keys
chmod 700 /home/ubuntu/.ssh
chmod 600 /home/ubuntu/.ssh/authorized_keys
chown -R ubuntu:ubuntu /home/ubuntu/.ssh

export DEBIAN_FRONTEND=noninteractive
apt-get update -q
apt-get install -y -q curl

curl -sfL https://get.k3s.io | \
  INSTALL_K3S_EXEC="--disable=traefik --disable=servicelb --write-kubeconfig-mode=644 --cluster-init" \
  sh -

# Wait for k3s to be ready (up to 3 minutes).
timeout 180 bash -c 'until k3s kubectl get nodes 2>/dev/null | grep -q " Ready"; do sleep 3; done'

echo "k3s installation complete: $(k3s --version | head -1)"
