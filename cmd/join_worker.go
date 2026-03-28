package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fatih/color"
	"github.com/rothgar/k3s-to-talos/internal/nextboot"
	"github.com/rothgar/k3s-to-talos/internal/ssh"
	"github.com/rothgar/k3s-to-talos/internal/talos"
	"github.com/spf13/cobra"
)

var (
	flagWorkerTalosVersion string
	flagWorkerConfig       string
	flagTalosConfig        string
)

var joinWorkerCmd = &cobra.Command{
	Use:   "join-worker",
	Short: "Convert a k3s agent node to a Talos worker and join an existing Talos cluster",
	Long: `Installs Talos on a k3s agent (worker) node and joins it to an existing
Talos cluster that was previously migrated with the 'migrate' command.

The --worker-config and --talosconfig flags must point to files generated
during the control plane migration (in <backup-dir>/talos-config/).

Steps:
  1. SSH into the worker node and run the nextboot agent (erases the OS,
     writes the Talos disk image, reboots)
  2. Wait for the Talos API on port 50000
  3. Apply the worker configuration (insecure, maintenance mode)
  4. Wait for the CA-verified gRPC API — the worker joins the cluster
     automatically via the cluster token embedded in worker.yaml

This process is IRREVERSIBLE. The worker node's OS will be erased.`,
	RunE: runJoinWorker,
}

func init() {
	joinWorkerCmd.Flags().StringVar(&flagWorkerTalosVersion, "talos-version", "v1.7.0", "Talos Linux version to install")
	joinWorkerCmd.Flags().StringVar(&flagWorkerConfig, "worker-config", "", "Path to worker.yaml from the control plane migration (required)")
	joinWorkerCmd.Flags().StringVar(&flagTalosConfig, "talosconfig", "", "Path to talosconfig from the control plane migration (required)")
	_ = joinWorkerCmd.MarkFlagRequired("worker-config")
	_ = joinWorkerCmd.MarkFlagRequired("talosconfig")
}

func runJoinWorker(cmd *cobra.Command, args []string) error {
	if flagHost == "" {
		return fmt.Errorf("--host is required")
	}

	if err := os.MkdirAll(flagBackupDir, 0750); err != nil {
		return fmt.Errorf("creating backup directory: %w", err)
	}

	color.Blue("\n══ Joining worker node %s to Talos cluster ══\n\n", flagHost)

	// ── Phase 1: Deploy Talos via nextboot ───────────────────────────────────
	color.Blue("[1/2] Deploying Talos to worker node via nextboot agent\n")

	sshClient, err := ssh.NewClient(ssh.Options{
		Host:    flagHost,
		Port:    flagSSHPort,
		User:    flagSSHUser,
		KeyPath: flagSSHKey,
		Sudo:    flagSudo,
	})
	if err != nil {
		return fmt.Errorf("SSH connection to worker failed: %w", err)
	}

	installer := nextboot.NewInstaller(sshClient, flagBackupDir)
	installErr := installer.Run(nextboot.Options{
		TalosVersion: flagWorkerTalosVersion,
		ConfigFile:   filepath.Clean(flagWorkerConfig),
	})
	sshClient.Close()

	if installErr != nil && !ssh.IsDisconnectError(installErr) {
		return fmt.Errorf("nextboot on worker failed: %w", installErr)
	}
	if installErr != nil {
		color.Yellow("SSH connection closed (worker is rebooting) — this is expected.\n")
	}

	// ── Phase 2: Bootstrap worker Talos ──────────────────────────────────────
	color.Blue("\n[2/2] Waiting for worker Talos to boot and join the cluster\n")

	bootstrapper := talos.NewBootstrapper(flagBackupDir)
	if err := bootstrapper.BootstrapWorker(talos.WorkerBootstrapOptions{
		Host:            flagHost,
		TalosConfigFile: filepath.Clean(flagTalosConfig),
		WorkerCfgFile:   filepath.Clean(flagWorkerConfig),
	}); err != nil {
		return fmt.Errorf("bootstrapping worker: %w", err)
	}

	color.Green("\n✓ Worker node %s is now running Talos and has joined the cluster.\n", flagHost)
	fmt.Printf("\nVerify with:\n  talosctl --talosconfig %s get members\n", flagTalosConfig)
	return nil
}
