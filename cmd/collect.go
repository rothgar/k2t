package cmd

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/rothgar/k3s-to-talos/internal/k3s"
	"github.com/rothgar/k3s-to-talos/internal/ssh"
	"github.com/rothgar/k3s-to-talos/internal/ui"
	"github.com/spf13/cobra"
)

var flagCollectMigrateToEtcd bool

var collectCmd = &cobra.Command{
	Use:   "collect [[user@]host]",
	Short: "Collect k3s cluster info and create a backup (no migration)",
	Long: `Connects to the remote k3s server via SSH and collects cluster information
including nodes, workloads, secrets, configmaps, and persistent volumes.
Also backs up the k3s database (etcd snapshot or SQLite file) and exports
all Kubernetes resources as YAML.

By default this command does NOT modify the remote machine.  When
--migrate-to-etcd is set the k3s datastore is converted from SQLite to
embedded etcd before the backup is taken; this restarts k3s briefly.`,
	RunE: runCollect,
}

func init() {
	collectCmd.Flags().BoolVar(&flagCollectMigrateToEtcd, "migrate-to-etcd", false,
		"Convert the k3s SQLite datastore to embedded etcd before backup (requires k3s restart)")
}

func runCollect(cmd *cobra.Command, args []string) error {
	target := resolveTarget(args)
	if target == "" {
		return fmt.Errorf("SSH target is required: k2t collect [user@]host")
	}

	if err := os.MkdirAll(flagBackupDir, 0750); err != nil {
		return fmt.Errorf("creating backup directory: %w", err)
	}

	ui.PrintPhaseHeader(1, "COLLECT", "Connecting to k3s node and backing up cluster state")

	sshClient, err := ssh.NewClient(sshOpts(target))
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}
	defer sshClient.Close()

	collector, err := k3s.Detect(sshClient)
	if err != nil {
		return fmt.Errorf("detecting cluster type: %w", err)
	}
	info, err := collector.Collect()
	if err != nil {
		return fmt.Errorf("collecting k3s info: %w", err)
	}

	// If the cluster is using SQLite and --migrate-to-etcd is set, convert
	// before taking the backup so we get a proper etcd snapshot.
	// Without the flag, collect still works: it backs up state.db and the
	// YAML resource export.  (The etcd snapshot is only required for Talos
	// restore; migrate.go enforces that separately.)
	if info.DatastoreType == "sqlite" && info.ClusterType != "kubeadm" && flagCollectMigrateToEtcd {
		if err := k3s.MigrateToEtcd(sshClient); err != nil {
			return fmt.Errorf("converting k3s to embedded etcd: %w", err)
		}
		collector2, err2 := k3s.Detect(sshClient)
		if err2 != nil {
			return fmt.Errorf("re-detecting cluster type after etcd migration: %w", err2)
		}
		info, err = collector2.Collect()
		if err != nil {
			return fmt.Errorf("re-collecting cluster info after etcd migration: %w", err)
		}
		if info.DatastoreType != "etcd" {
			return fmt.Errorf("k3s still reports SQLite after --cluster-init migration; check k3s logs")
		}
		color.Green("  ✓ Datastore converted to embedded etcd\n")
	}

	backup := k3s.NewBackup(sshClient, flagBackupDir, sshOpts(target).Host)
	if err := backup.Run(info, false); err != nil {
		return fmt.Errorf("backing up k3s data: %w", err)
	}

	ui.PrintClusterSummary(info, flagBackupDir)

	if len(info.Nodes) > 1 {
		ui.PrintMultiNodeWarning(info.Nodes)
	}

	fmt.Printf("\nBackup saved to: %s\n", flagBackupDir)
	return nil
}
