package nextboot

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
	"github.com/rothgar/k2t/internal/ssh"
	"github.com/rothgar/k2t/internal/talos"
)

// Options holds parameters for the nextboot-talos installer.
type Options struct {
	TalosVersion   string
	ControlPlaneIP string
	ConfigFile     string              // path to local controlplane.yaml
	Hardware       *talos.HardwareInfo // detected hardware; nil defaults to amd64
}

// Installer uploads the k3s-to-talos binary to the remote machine and runs
// the hidden "nextboot" subcommand on it to install Talos in-place.
type Installer struct {
	ssh       *ssh.Client
	backupDir string
}

// NewInstaller creates a new Installer.
func NewInstaller(sshClient *ssh.Client, backupDir string) *Installer {
	return &Installer{ssh: sshClient, backupDir: backupDir}
}

// Run uploads the binary + machine config and executes the nextboot agent on
// the remote machine.
func (i *Installer) Run(opts Options) error {
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)

	// ── 1. Resolve Talos image URL ────────────────────────────────────────────
	s.Suffix = " Resolving Talos image URL..."
	s.Start()

	hw := opts.Hardware
	if hw == nil {
		hw = &talos.HardwareInfo{Arch: talos.ArchAMD64}
	}

	imageURL, imageHash, err := talos.ResolveImageURL(opts.TalosVersion, hw)
	if err != nil {
		s.Stop()
		color.Yellow("  Warning: could not resolve image URL (%v); using amd64 default.\n", err)
		imageURL = fmt.Sprintf(
			"https://factory.talos.dev/image/376567988ad370138ad8b2698212367b8edcb69b5fd68c80be1f2ec7d603b4ba/%s/metal-amd64.raw.zst",
			opts.TalosVersion,
		)
		imageHash = ""
	}
	s.Stop()
	fmt.Printf("  Image URL:  %s\n", imageURL)
	if imageHash != "" {
		fmt.Printf("  SHA-256:    %s\n", imageHash)
	}

	// ── 2. Locate the binary to upload ────────────────────────────────────────
	s.Suffix = " Locating agent binary..."
	s.Start()

	binaryPath, tmpBinary, err := resolveAgentBinary()
	if err != nil {
		s.Stop()
		return fmt.Errorf("resolving agent binary: %w", err)
	}
	if tmpBinary != "" {
		defer os.Remove(tmpBinary)
	}
	s.Stop()
	fmt.Printf("  Agent binary: %s\n", binaryPath)

	// ── 3. Upload binary ──────────────────────────────────────────────────────
	s.Suffix = " Uploading nextboot agent binary..."
	s.Start()

	const remoteBin = "/tmp/k3s-to-talos-nextboot"
	if err := i.ssh.Upload(binaryPath, remoteBin); err != nil {
		s.Stop()
		return fmt.Errorf("uploading agent binary: %w", err)
	}
	if _, err := i.ssh.Run(fmt.Sprintf("chmod +x %s", remoteBin)); err != nil {
		s.Stop()
		return fmt.Errorf("chmod agent binary: %w", err)
	}
	s.Stop()
	fmt.Printf("  ✓ Binary uploaded to %s\n", remoteBin)

	// ── 4. Upload machine config ──────────────────────────────────────────────
	const remoteCfg = "/tmp/nextboot-config.yaml"
	if opts.ConfigFile != "" {
		s.Suffix = " Uploading machine config..."
		s.Start()
		if err := i.ssh.Upload(opts.ConfigFile, remoteCfg); err != nil {
			s.Stop()
			color.Yellow("  Warning: could not upload config file: %v\n", err)
			color.Yellow("  Talos will boot in maintenance mode.\n")
		} else {
			s.Stop()
			fmt.Printf("  ✓ Machine config uploaded to %s\n", remoteCfg)
		}
	}

	// ── 5. Execute nextboot agent on the remote ───────────────────────────────
	color.Red("\n  !! POINT OF NO RETURN — executing nextboot agent !!\n")
	color.Red("  The remote machine will now be erased and rebooted into Talos.\n\n")

	remoteCmd := fmt.Sprintf("%s nextboot --image-url %q", remoteBin, imageURL)
	if imageHash != "" {
		remoteCmd += fmt.Sprintf(" --image-hash %q", imageHash)
	}
	if opts.ConfigFile != "" {
		remoteCmd += fmt.Sprintf(" --config %s", remoteCfg)
	}

	// Run the agent with a reboot-aware wrapper.  The agent ends with a
	// reboot syscall which kills the remote process instantly.  On real
	// hardware the TCP RST propagates and RunStream returns promptly, but
	// QEMU user-mode networking (SLiRP) keeps the forwarded TCP connection
	// alive after the guest reboots, so RunStream hangs forever.
	//
	// Solution: wrap stdout in a rebootDetector that fires a channel when it
	// sees "Rebooting into Talos" or "kexec -e".  A goroutine waits on
	// either RunStream completing normally or the reboot signal + grace
	// period, whichever comes first.
	detector := &rebootDetector{
		inner: newPrefixWriter("  remote> "),
		done:  make(chan struct{}),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- i.ssh.RunStream(
			remoteCmd,
			detector,
			newPrefixWriter("  remote> "),
		)
	}()

	select {
	case err = <-errCh:
		// Normal completion or disconnect.
	case <-detector.done:
		// The agent printed a reboot message.  Give it a few seconds for
		// the syscall to fire, then move on regardless.
		select {
		case err = <-errCh:
		case <-time.After(10 * time.Second):
			// RunStream is stuck (QEMU SLiRP keeps TCP alive). Close the
			// SSH session by closing the client — this unblocks RunStream.
			i.ssh.Close()
			err = <-errCh
		}
	}

	// SSH disconnect is expected — the machine reboots at the end of the agent.
	if err != nil && !ssh.IsDisconnectError(err) {
		return fmt.Errorf("nextboot agent failed: %w", err)
	}

	return nil
}

// resolveAgentBinary returns the path to a Linux amd64 k3s-to-talos binary
// suitable for uploading to the remote machine.
//
// If the current process is already a Linux amd64 binary (the typical CI
// case), it returns os.Executable().  Otherwise it cross-compiles a fresh
// binary using `go build`.
//
// The second return value is a temp file path that the caller must delete
// after use (empty string if os.Executable was used).
func resolveAgentBinary() (path string, tmpPath string, err error) {
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		p, err := os.Executable()
		if err != nil {
			return "", "", fmt.Errorf("os.Executable: %w", err)
		}
		return p, "", nil
	}

	// Cross-compile for Linux amd64.
	color.Yellow("  Note: current binary is %s/%s; cross-compiling for linux/amd64...\n",
		runtime.GOOS, runtime.GOARCH)

	tmp, err := os.CreateTemp("", "k3s-to-talos-linux-amd64-*")
	if err != nil {
		return "", "", err
	}
	tmp.Close()

	cmd := exec.Command("go", "build", "-o", tmp.Name(), ".")
	cmd.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH=amd64",
		"CGO_ENABLED=0",
	)
	if out, buildErr := cmd.CombinedOutput(); buildErr != nil {
		os.Remove(tmp.Name())
		return "", "", fmt.Errorf("cross-compile failed: %w\n%s", buildErr, string(out))
	}

	return tmp.Name(), tmp.Name(), nil
}

// prefixWriter is an io.Writer that prepends a prefix to every output line.
type prefixWriter struct {
	prefix string
	buf    []byte
}

func newPrefixWriter(prefix string) *prefixWriter {
	return &prefixWriter{prefix: prefix}
}

func (pw *prefixWriter) Write(p []byte) (n int, err error) {
	pw.buf = append(pw.buf, p...)
	for {
		idx := -1
		for j, b := range pw.buf {
			if b == '\n' {
				idx = j
				break
			}
		}
		if idx < 0 {
			break
		}
		fmt.Printf("%s%s\n", pw.prefix, pw.buf[:idx])
		pw.buf = pw.buf[idx+1:]
	}
	return len(p), nil
}

// rebootDetector wraps an io.Writer and signals on its done channel when it
// sees the nextboot agent's reboot message in the output stream.  This lets
// the caller stop waiting for RunStream to return — on QEMU user-mode
// networking the TCP connection stays alive after guest reboot, so the SSH
// session never gets an EOF.
type rebootDetector struct {
	inner io.Writer
	done  chan struct{}
	once  sync.Once
}

func (d *rebootDetector) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("Rebooting into Talos")) ||
		bytes.Contains(p, []byte("kexec -e")) ||
		bytes.Contains(p, []byte("Talos installation complete")) {
		d.once.Do(func() { close(d.done) })
	}
	return d.inner.Write(p)
}
