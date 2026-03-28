package talos

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
)

// GenerateOptions holds parameters for talosctl gen config.
type GenerateOptions struct {
	ClusterName    string
	ControlPlaneIP string
	TalosVersion   string
	OutputDir      string
	DryRun         bool
	// PodCIDR and ServiceCIDR override the default Talos network ranges so
	// they match the source cluster.  After etcd restore the old Flannel
	// network config (stored in etcd by the source cluster) must match what
	// Talos configures — otherwise Flannel crashes and pods cannot start.
	// Leave empty to use Talos defaults (10.244.0.0/16 / 10.96.0.0/12).
	PodCIDR     string // e.g. "10.42.0.0/16" (k3s default)
	ServiceCIDR string // e.g. "10.43.0.0/16" (k3s default)
}

// ConfigGenerator runs talosctl gen config to produce machine configs.
type ConfigGenerator struct {
	backupDir string
}

// NewConfigGenerator creates a new ConfigGenerator.
func NewConfigGenerator(backupDir string) *ConfigGenerator {
	return &ConfigGenerator{backupDir: backupDir}
}

// Generate runs talosctl gen config and writes output to the specified directory.
func (g *ConfigGenerator) Generate(opts GenerateOptions) error {
	// Check talosctl is available
	talosctlPath, err := exec.LookPath("talosctl")
	if err != nil {
		return fmt.Errorf(
			"talosctl not found in PATH\n\n"+
				"Install talosctl:\n"+
				"  curl -sL https://talos.dev/install | sh\n"+
				"Or download from: https://github.com/siderolabs/talos/releases\n",
		)
	}

	if err := os.MkdirAll(opts.OutputDir, 0750); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	endpoint := fmt.Sprintf("https://%s:6443", opts.ControlPlaneIP)

	// Strategic-merge patch applied to all generated machine configs.
	//
	// machine.certSANs — add the public IP so that CA-verified talosctl calls
	// via the public IP succeed after config is applied.  On EC2 (and other
	// cloud providers) the public IP is not on any interface, so Talos would
	// not auto-include it in the machined TLS cert SANs.
	//
	// cluster.network — use the source cluster's pod/service CIDRs.  After
	// etcd restore, Flannel finds its existing network config (stored by the
	// source cluster in etcd) and refuses to switch to a different CIDR.
	// Setting the same CIDRs avoids CNI failures that leave pods in
	// ContainerCreating indefinitely.  JSON6902 patches are not supported for
	// multi-document configs in talosctl v1.12, so we use a YAML
	// strategic-merge patch.
	podCIDR := opts.PodCIDR
	serviceCIDR := opts.ServiceCIDR
	configPatch := fmt.Sprintf("machine:\n  certSANs:\n    - %q\n", opts.ControlPlaneIP)
	if podCIDR != "" || serviceCIDR != "" {
		configPatch += "cluster:\n  network:\n"
		if podCIDR != "" {
			configPatch += fmt.Sprintf("    podSubnets:\n      - %q\n", podCIDR)
		}
		if serviceCIDR != "" {
			configPatch += fmt.Sprintf("    serviceSubnets:\n      - %q\n", serviceCIDR)
		}
	}

	args := []string{
		"gen", "config",
		opts.ClusterName,
		endpoint,
		"--output", opts.OutputDir,
		"--output-types", "controlplane,worker,talosconfig",
		"--config-patch", configPatch,
		"--force",
	}

	if opts.TalosVersion != "" {
		args = append(args, "--talos-version", opts.TalosVersion)
	}

	if opts.DryRun {
		color.Yellow("[DRY RUN] Would run: %s %s\n", talosctlPath, strings.Join(args, " "))
		// Create placeholder files for dry-run so subsequent steps have something to reference
		for _, name := range []string{"controlplane.yaml", "worker.yaml", "talosconfig"} {
			placeholder := filepath.Join(opts.OutputDir, name)
			os.WriteFile(placeholder, []byte("# dry-run placeholder\n"), 0600) //nolint:errcheck
		}
		return nil
	}

	cmd := exec.Command(talosctlPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("talosctl gen config failed: %w\n%s", err, stderr.String())
	}

	// Verify expected files were created
	for _, name := range []string{"controlplane.yaml", "worker.yaml", "talosconfig"} {
		p := filepath.Join(opts.OutputDir, name)
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("expected output file not found: %s", p)
		}
	}

	color.Green("  ✓ controlplane.yaml → %s\n", filepath.Join(opts.OutputDir, "controlplane.yaml"))
	color.Green("  ✓ worker.yaml       → %s\n", filepath.Join(opts.OutputDir, "worker.yaml"))
	color.Green("  ✓ talosconfig       → %s\n", filepath.Join(opts.OutputDir, "talosconfig"))

	return nil
}
