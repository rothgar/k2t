# hack/local-test.sh — Local KVM Testing Framework

Mirrors the EC2 CI workflow for `k3s-to-talos` using QEMU/KVM VMs and
cloud-init on your local machine. Same migration logic, different infra layer.

## Quick Start

```bash
# Install prerequisites (Ubuntu/Debian)
sudo apt-get install -y qemu-kvm qemu-utils ovmf cloud-image-utils talosctl kubectl

# Run the default test (k3s single-node migrate)
./hack/local-test.sh

# Run a specific test type
./hack/local-test.sh k3s-multi
./hack/local-test.sh kubeadm-single

# Keep VMs alive after the test for debugging
./hack/local-test.sh --keep-vms k3s-single

# Print full setup instructions (including bridge networking)
./hack/local-test.sh --setup

# Clean cached images and stale temp dirs
./hack/local-test.sh --clean
```

## Prerequisites

| Requirement         | Single-node | Multi-node | Notes |
|---------------------|:-----------:|:----------:|-------|
| `qemu-kvm`          | required    | required   | KVM acceleration |
| `qemu-utils`        | required    | required   | `qemu-img` overlay disks |
| `ovmf`              | required    | required   | UEFI firmware for Talos |
| `cloud-image-utils` | required    | required   | `cloud-localds` ISO tool |
| `talosctl`          | required    | required   | Talos control plane |
| `kubectl`           | required    | required   | Post-migration checks |
| `libvirt` + `virbr0`| optional    | **required** | Bridge networking |
| qemu-bridge-helper  | optional    | **required** | `/etc/qemu/bridge.conf` |

Run `./hack/local-test.sh --setup` for copy-pasteable install commands.

## Test Types

| Type              | Est. Duration | Nodes | Networking |
|-------------------|:-------------:|:-----:|------------|
| `k3s-single`      | ~20 min       | 1     | user-mode or bridge |
| `k3s-multi`       | ~35 min       | 2     | bridge only |
| `kubeadm-single`  | ~25 min       | 1     | user-mode or bridge |
| `kubeadm-multi`   | ~40 min       | 2     | bridge only |
| `all`             | ~2 hrs        | 1–2   | requires bridge |

Timings assume a warm base-image cache and a modern host (NVMe, 8+ CPU cores).

## Networking Modes

**Bridge mode** (auto-detected when `virbr0` + bridge helper are configured):
VMs get static IPs (`192.168.122.10/11`) on libvirt's default NAT network.
The host can reach them directly. Required for multi-node tests.

**User-mode** (automatic fallback for single-node):
QEMU forwards host ports → VM: SSH→10022, Talos API→10500, K8s→10443.
No libvirt required.

## Debugging Tips

**Serial console** — the most useful log for boot failures:
```bash
tail -f /tmp/k3s-to-talos-test-<PID>/serial-cp.log
```

**Keep VMs running** after a failure to SSH in and inspect:
```bash
./hack/local-test.sh --keep-vms k3s-single
# Bridge mode:
ssh -i /tmp/k3s-to-talos-test-<PID>/ci_key ubuntu@192.168.122.10
# User-mode:
ssh -i /tmp/k3s-to-talos-test-<PID>/ci_key -p 10022 ubuntu@127.0.0.1
```

**Migration log** at `${WORK_DIR}/migrate.log`; join-worker log at `join-worker.log`.

**Talos version override:**
```bash
./hack/local-test.sh --talos-version v1.12.6 k3s-single
```

**Use a pre-built binary:**
```bash
go build -o ./k3s-to-talos . && ./hack/local-test.sh --no-build k3s-single
```

## How It Differs from EC2 CI

- **Infrastructure**: QEMU/KVM VMs on your laptop vs. EC2 instances in AWS
- **Networking**: libvirt NAT bridge or QEMU user-mode vs. VPC subnets
- **Image**: Ubuntu 24.04 cloud image (same OS) booted via cloud-init
- **UEFI**: OVMF pflash drives required locally (Talos EFI installer)
- **Logic**: identical — same `.github/scripts/` user-data, same binary, same flags
