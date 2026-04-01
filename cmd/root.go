package cmd

import (
	"strings"

	"github.com/rothgar/k3s-to-talos/internal/ssh"
	"github.com/spf13/cobra"
)

// Global flags shared across commands.
var (
	flagHost      string
	flagSSHKey    string
	flagSSHPort   int
	flagBackupDir string
)

var rootCmd = &cobra.Command{
	Use:   "k2t",
	Short: "Migrate a k3s or kubeadm server node to Talos Linux",
	Long: `k2t is a CLI tool that remotely migrates a machine running k3s or kubeadm
in server (control-plane) mode to Talos Linux.

It connects to the remote machine over SSH, collects cluster information,
backs up the database and Kubernetes resources, generates Talos machine
configs, and then uses nextboot-talos to erase and reboot the machine into
Talos Linux.

The SSH target accepts standard SSH notation: [user@]host

  k2t migrate ubuntu@10.1.1.1
  k2t migrate --host ubuntu@10.1.1.1

When the SSH user is not "root", sudo is used automatically for privileged
commands.

WARNING: This process is IRREVERSIBLE. The target machine's OS will be
completely replaced. Ensure you have backed up all critical data before
proceeding.`,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

// resolveTarget returns the SSH target ([user@]host) from the first positional
// argument if provided, otherwise falls back to --host.
func resolveTarget(args []string) string {
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}
	return flagHost
}

// sshOpts parses a [user@]host target string and returns ssh.Options.
// Sudo is auto-detected by ssh.NewClient based on the resolved user.
func sshOpts(target string) ssh.Options {
	user := "root"
	host := target
	if idx := strings.Index(target, "@"); idx >= 0 {
		user = target[:idx]
		host = target[idx+1:]
	}
	return ssh.Options{
		Host:    host,
		Port:    flagSSHPort,
		User:    user,
		KeyPath: flagSSHKey,
	}
}

// resolveHost returns just the host portion of a [user@]host target.
func resolveHost(args []string) string {
	return sshOpts(resolveTarget(args)).Host
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagHost, "host", "", "SSH target: [user@]host (e.g. ubuntu@10.1.1.1)")
	rootCmd.PersistentFlags().StringVar(&flagSSHKey, "ssh-key", "", "Path to SSH private key (defaults to ~/.ssh/id_rsa)")
	rootCmd.PersistentFlags().IntVar(&flagSSHPort, "ssh-port", 22, "SSH port")
	rootCmd.PersistentFlags().StringVar(&flagBackupDir, "backup-dir", "./k3s-backup", "Local directory for backups and generated configs")

	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(joinWorkerCmd)
	rootCmd.AddCommand(collectCmd)
	rootCmd.AddCommand(generateCmd)
}
