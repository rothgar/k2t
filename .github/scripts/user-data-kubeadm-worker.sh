#!/bin/bash
# cloud-init user-data: install kubeadm prerequisites on a worker node.
# Does NOT run kubeadm init or join — the join command is delivered via SSH
# after the control plane is ready.
set -euo pipefail

# Set up SSH key for the ubuntu user immediately so the runner can connect
# while the rest of the installation proceeds.
mkdir -p /home/ubuntu/.ssh
echo "__SSH_PUBLIC_KEY__" >> /home/ubuntu/.ssh/authorized_keys
chmod 700 /home/ubuntu/.ssh
chmod 600 /home/ubuntu/.ssh/authorized_keys
chown -R ubuntu:ubuntu /home/ubuntu/.ssh

export DEBIAN_FRONTEND=noninteractive

# Disable swap (required by kubelet).
swapoff -a
sed -i '/swap/d' /etc/fstab

# Load required kernel modules.
cat > /etc/modules-load.d/k8s.conf <<'EOF'
overlay
br_netfilter
EOF
modprobe overlay
modprobe br_netfilter

cat > /etc/sysctl.d/k8s.conf <<'EOF'
net.bridge.bridge-nf-call-iptables  = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward                 = 1
EOF
sysctl --system

# ── containerd ─────────────────────────────────────────────────────────────────
apt-get update -q
apt-get install -y -q apt-transport-https ca-certificates curl gpg containerd

mkdir -p /etc/containerd
containerd config default > /etc/containerd/config.toml
sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml
systemctl restart containerd
systemctl enable containerd

# ── kubeadm / kubelet / kubectl ────────────────────────────────────────────────
K8S_VERSION="v1.30"

mkdir -p /etc/apt/keyrings
curl -fsSL "https://pkgs.k8s.io/core:/stable:/${K8S_VERSION}/deb/Release.key" \
  | gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg

echo "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] \
  https://pkgs.k8s.io/core:/stable:/${K8S_VERSION}/deb/ /" \
  > /etc/apt/sources.list.d/kubernetes.list

apt-get update -q
apt-get install -y -q kubelet kubeadm kubectl
apt-mark hold kubelet kubeadm kubectl

systemctl enable kubelet

echo "kubeadm worker prerequisites installed: $(kubelet --version)"
