#!/bin/bash
# cloud-init user-data: install k3s (server mode) and enable root SSH.
# This script is templated by the EC2 workflow: __SSH_PUBLIC_KEY__ is replaced
# with the actual public key before the instance is launched.
set -euo pipefail

# ── Root SSH access ────────────────────────────────────────────────────────────
# EC2 default users (ubuntu, admin, ec2-user) get the key injected by AWS;
# we copy it to root so k3s-to-talos can connect as root.
mkdir -p /root/.ssh
chmod 700 /root/.ssh

# Copy from whatever default user AWS set up.
for dir in /home/ubuntu /home/admin /home/ec2-user; do
  if [ -f "$dir/.ssh/authorized_keys" ]; then
    cat "$dir/.ssh/authorized_keys" >> /root/.ssh/authorized_keys
    break
  fi
done

# Also add the workflow-generated key directly.
echo "__SSH_PUBLIC_KEY__" >> /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys

# Enable PermitRootLogin in sshd.
for f in /etc/ssh/sshd_config /etc/ssh/sshd_config.d/60-cloudimg-settings.conf; do
  [ -f "$f" ] && sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin yes/' "$f"
done
systemctl restart sshd 2>/dev/null || service sshd restart 2>/dev/null || true

# ── k3s install ────────────────────────────────────────────────────────────────
export DEBIAN_FRONTEND=noninteractive
apt-get update -q
apt-get install -y -q curl

curl -sfL https://get.k3s.io | \
  INSTALL_K3S_EXEC="--disable=traefik --disable=servicelb --write-kubeconfig-mode=644" \
  sh -

# Wait for k3s to be ready (up to 3 minutes).
timeout 180 bash -c 'until k3s kubectl get nodes 2>/dev/null | grep -q " Ready"; do sleep 3; done'

echo "k3s installation complete: $(k3s --version | head -1)"
