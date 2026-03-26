#!/bin/bash
# cloud-init user-data: install kubeadm and initialise a single-node cluster.
# This script is templated by the EC2 workflow: __SSH_PUBLIC_KEY__ is replaced
# with the actual public key before the instance is launched.
set -euo pipefail

# ── Root SSH access ────────────────────────────────────────────────────────────
mkdir -p /root/.ssh
chmod 700 /root/.ssh
for dir in /home/ubuntu /home/admin /home/ec2-user; do
  if [ -f "$dir/.ssh/authorized_keys" ]; then
    cat "$dir/.ssh/authorized_keys" >> /root/.ssh/authorized_keys
    break
  fi
done
echo "__SSH_PUBLIC_KEY__" >> /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys

for f in /etc/ssh/sshd_config /etc/ssh/sshd_config.d/60-cloudimg-settings.conf; do
  [ -f "$f" ] && sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin yes/' "$f"
done
systemctl restart sshd 2>/dev/null || service sshd restart 2>/dev/null || true

# ── System preparation ─────────────────────────────────────────────────────────
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

# ── Cluster initialisation ─────────────────────────────────────────────────────
# Get the primary private IP for the advertise address.
ADVERTISE_IP=$(hostname -I | awk '{print $1}')

kubeadm init \
  --apiserver-advertise-address="${ADVERTISE_IP}" \
  --pod-network-cidr=10.244.0.0/16 \
  --cri-socket=unix:///run/containerd/containerd.sock \
  --ignore-preflight-errors=NumCPU

# Copy admin config for root kubectl use.
mkdir -p /root/.kube
cp /etc/kubernetes/admin.conf /root/.kube/config

# Allow scheduling on the control-plane node (single-node cluster).
kubectl taint nodes --all node-role.kubernetes.io/control-plane:NoSchedule- 2>/dev/null || true

# Install Flannel CNI.
kubectl apply -f \
  https://raw.githubusercontent.com/flannel-io/flannel/master/Documentation/kube-flannel.yml

# Wait for the node to become Ready (up to 5 minutes).
timeout 300 bash -c \
  'until kubectl get nodes 2>/dev/null | grep -q " Ready"; do sleep 5; done'

echo "kubeadm installation complete: $(kubelet --version)"
