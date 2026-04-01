package k3s

import (
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/rothgar/k3s-to-talos/internal/ssh"
)

// MigrateToEtcd converts a k3s server running SQLite to embedded etcd using
// the k3s --cluster-init mechanism.  After the migration, k3s restarts with
// embedded etcd so the caller can take a proper etcd snapshot.
//
// The migration works as follows:
//  1. Append `cluster-init: true` to /etc/rancher/k3s/config.yaml
//  2. Stop k3s cleanly, then start it (the config triggers SQLite→etcd migration)
//  3. Poll until the etcd/member directory appears (up to 5 min)
//  4. Remove `cluster-init` from config.yaml so subsequent restarts are clean
//  5. Restart k3s once more to verify it runs cleanly without the flag
func MigrateToEtcd(sshClient *ssh.Client) error {
	color.Blue("  Converting k3s SQLite datastore to embedded etcd...\n")

	const k3sCfg = "/etc/rancher/k3s/config.yaml"

	// ── Step 1: inject cluster-init into k3s config ───────────────────────
	// k3s reads /etc/rancher/k3s/config.yaml and merges it with CLI flags.
	// Appending `cluster-init: true` is cleaner than a systemd dropin because
	// we don't need to parse or rewrite the ExecStart line.

	// Check whether cluster-init is already present (idempotent).
	existing, _ := sshClient.Run(fmt.Sprintf("cat %s 2>/dev/null", k3sCfg))
	if strings.Contains(existing, "cluster-init") {
		color.Yellow("  k3s config already has cluster-init — checking etcd member dir\n")
	} else {
		if _, err := sshClient.Run("mkdir -p /etc/rancher/k3s"); err != nil {
			return fmt.Errorf("creating k3s config directory: %w", err)
		}
		// Wrap in sh -c so the >> redirection runs as root, not as the SSH
		// user.  A bare `sudo echo ... >> file` fails because the shell opens
		// the file before sudo elevates privileges.
		if _, err := sshClient.Run(
			`sh -c 'echo "cluster-init: true" >> /etc/rancher/k3s/config.yaml'`,
		); err != nil {
			return fmt.Errorf("writing cluster-init to k3s config: %w", err)
		}

		// ── Step 2: stop k3s cleanly then start with new config ──────────────
		// Use stop+start rather than restart: this guarantees all file locks
		// (SQLite WAL, etcd member lock) are released before the new instance
		// tries to initialise embedded etcd.
		color.Blue("  Stopping k3s (clean shutdown before etcd initialisation)...\n")
		if _, err := sshClient.Run("systemctl stop k3s"); err != nil {
			// Non-fatal: k3s may not be running under systemd in all envs.
			color.Yellow("  Warning: systemctl stop k3s: %v — will try start anyway\n", err)
		}
		// Brief pause to let OS release any lingering file locks.
		time.Sleep(2 * time.Second)

		color.Blue("  Starting k3s with --cluster-init (migrates SQLite → etcd)...\n")
		if _, err := sshClient.Run("systemctl start k3s"); err != nil {
			// Capture journalctl for diagnostics.
			journal, _ := sshClient.Run("journalctl -u k3s -n 50 --no-pager 2>/dev/null")
			return fmt.Errorf("starting k3s with cluster-init: %w\n\nk3s journal:\n%s", err, journal)
		}
	}

	// ── Step 3: poll for etcd/member ──────────────────────────────────────
	color.Blue("  Waiting for embedded etcd to initialise (up to 5 min)...\n")
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		if sshClient.FileExists("/var/lib/rancher/k3s/server/db/etcd/member") {
			color.Green("  ✓ Embedded etcd is running\n")
			break
		}
		time.Sleep(5 * time.Second)
	}
	if !sshClient.FileExists("/var/lib/rancher/k3s/server/db/etcd/member") {
		journal, _ := sshClient.Run("journalctl -u k3s -n 100 --no-pager 2>/dev/null")
		return fmt.Errorf(
			"timed out waiting for embedded etcd (etcd/member not found after 5 min)\n\nk3s journal:\n%s",
			journal)
	}

	// ── Step 4: remove cluster-init from config ────────────────────────────
	// k3s docs: remove --cluster-init after the first successful migration
	// so that restarts don't reinitialise the cluster.
	if _, err := sshClient.Run(
		fmt.Sprintf("sed -i '/^cluster-init/d' %s", k3sCfg),
	); err != nil {
		color.Yellow("  Warning: could not remove cluster-init from %s: %v\n", k3sCfg, err)
		color.Yellow("  Remove it manually before the next k3s restart.\n")
	}

	// ── Step 5: restart k3s without cluster-init ──────────────────────────
	color.Blue("  Restarting k3s without cluster-init to verify clean startup...\n")
	if _, err := sshClient.Run("systemctl restart k3s"); err != nil {
		journal, _ := sshClient.Run("journalctl -u k3s -n 50 --no-pager 2>/dev/null")
		return fmt.Errorf("restarting k3s after removing cluster-init: %w\n\nk3s journal:\n%s", err, journal)
	}
	time.Sleep(10 * time.Second)

	if !sshClient.FileExists("/var/lib/rancher/k3s/server/db/etcd/member") {
		return fmt.Errorf("etcd/member directory disappeared after clean restart; k3s may have fallen back to SQLite")
	}

	color.Green("  ✓ k3s is now running with embedded etcd\n")
	return nil
}
