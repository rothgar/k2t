package k3s

import (
	"fmt"
	"strings"

	"github.com/rothgar/k3s-to-talos/internal/ssh"
)

// Cluster type constants.
const (
	ClusterTypeK3s     = "k3s"
	ClusterTypeKubeadm = "kubeadm"
)

// Detect inspects the remote machine and returns a Collector pre-configured
// for the detected Kubernetes distribution.
//
// Detection order:
//  1. k3s   — checked first (k3s server process or /etc/rancher/k3s/ config)
//  2. kubeadm — kubelet service + /etc/kubernetes/admin.conf
//
// Returns an error when neither distribution is found.
func Detect(client *ssh.Client) (*Collector, error) {
	if hasK3sServer(client) {
		return &Collector{ssh: client, clusterType: ClusterTypeK3s}, nil
	}
	if hasKubeadm(client) {
		return &Collector{ssh: client, clusterType: ClusterTypeKubeadm}, nil
	}
	return nil, fmt.Errorf(
		"no supported Kubernetes distribution found on the target machine\n\n" +
			"Supported distributions:\n" +
			"  • k3s      — requires k3s running in server (control-plane) mode\n" +
			"  • kubeadm  — requires kubelet active and /etc/kubernetes/admin.conf present\n\n" +
			"Ensure the node is a control-plane node (not a worker/agent only).",
	)
}

// hasK3sServer returns true when the remote machine is running k3s in server mode.
func hasK3sServer(client *ssh.Client) bool {
	// Process check works on both systemd and non-systemd hosts.
	out, _ := client.Run(
		`systemctl is-active k3s 2>/dev/null || ` +
			`systemctl is-active k3s-server 2>/dev/null || ` +
			`(pgrep -f 'k3s server' >/dev/null 2>&1 && echo active) || ` +
			`echo inactive`)
	if strings.TrimSpace(out) == "inactive" {
		return false
	}
	// Also confirm server config files exist (not just an agent).
	return client.FileExists("/etc/rancher/k3s/k3s.yaml") ||
		client.FileExists("/var/lib/rancher/k3s/server")
}

// hasKubeadm returns true when the remote machine is a kubeadm control-plane node.
func hasKubeadm(client *ssh.Client) bool {
	// kubelet must be running.
	out, _ := client.Run(
		`systemctl is-active kubelet 2>/dev/null || ` +
			`(pgrep -f kubelet >/dev/null 2>&1 && echo active) || ` +
			`echo inactive`)
	if strings.TrimSpace(out) == "inactive" {
		return false
	}
	// admin.conf is only present on control-plane nodes.
	return client.FileExists("/etc/kubernetes/admin.conf")
}
