package network

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	iptablesRulesPath = "/etc/iptables/slipgate-rules.v4"
	iptablesUnitPath  = "/etc/systemd/system/slipgate-iptables.service"
)

// iptables-restore is invoked early at boot to re-apply rules saved by
// iptablesPersist. %s placeholders are filled with the absolute restore-binary
// path and the rules file path at write time.
const iptablesUnitTemplate = `[Unit]
Description=Restore slipgate iptables rules
DefaultDependencies=no
Before=network-pre.target
Wants=network-pre.target

[Service]
Type=oneshot
ExecStart=%s %s
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`

// HostFirewallActive reports whether the host is actually filtering inbound
// traffic. Used to decide whether to hint about external/cloud firewalls —
// merely having an iptables binary installed (e.g., pulled in by Docker or
// fail2ban) does not count if the INPUT chain still defaults to ACCEPT.
func HostFirewallActive() bool {
	if _, err := exec.LookPath("ufw"); err == nil && ufwActive() {
		return true
	}
	if _, err := exec.LookPath("firewall-cmd"); err == nil && firewalldActive() {
		return true
	}
	if _, err := exec.LookPath("iptables"); err == nil && iptablesFiltering() {
		return true
	}
	return false
}

// iptablesFiltering reports whether the INPUT chain is actually blocking
// traffic — either the default policy isn't ACCEPT, or there's at least one
// DROP/REJECT rule. ACCEPT-only chains don't filter anything.
func iptablesFiltering() bool {
	out, err := exec.Command("iptables", "-S", "INPUT").Output()
	if err != nil {
		return false
	}
	for _, line := range splitLines(string(out)) {
		if strings.HasPrefix(line, "-P INPUT ") {
			if !strings.HasSuffix(line, " ACCEPT") {
				return true
			}
			continue
		}
		if strings.Contains(line, "-j DROP") || strings.Contains(line, "-j REJECT") {
			return true
		}
	}
	return false
}

// AllowPort opens a port using the first available firewall tool.
func AllowPort(port int, proto string) error {
	// Try UFW first — but only if it's active
	if _, err := exec.LookPath("ufw"); err == nil && ufwActive() {
		return ufwAllow(port, proto)
	}

	// Try firewall-cmd — but only if it's running
	if _, err := exec.LookPath("firewall-cmd"); err == nil && firewalldActive() {
		return firewalldAllow(port, proto)
	}

	// Fallback to iptables
	if _, err := exec.LookPath("iptables"); err == nil {
		return iptablesAllow(port, proto)
	}

	// No active firewall — port is already reachable, nothing to do.
	return nil
}

// RemovePort removes a firewall rule for a port.
func RemovePort(port int, proto string) error {
	if _, err := exec.LookPath("ufw"); err == nil && ufwActive() {
		return run("ufw", "delete", "allow", fmt.Sprintf("%d/%s", port, proto))
	}
	if _, err := exec.LookPath("firewall-cmd"); err == nil && firewalldActive() {
		_ = run("firewall-cmd", "--permanent", "--remove-port", fmt.Sprintf("%d/%s", port, proto))
		return run("firewall-cmd", "--reload")
	}
	if _, err := exec.LookPath("iptables"); err == nil {
		if err := run("iptables", "-D", "INPUT", "-p", proto, "--dport", fmt.Sprintf("%d", port), "-j", "ACCEPT"); err != nil {
			return err
		}
		_ = iptablesPersist()
		return nil
	}
	return nil
}

// ufwActive checks if UFW is active (not just installed).
func ufwActive() bool {
	out, err := exec.Command("ufw", "status").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Status: active")
}

// firewalldActive checks if firewalld is running.
func firewalldActive() bool {
	return exec.Command("firewall-cmd", "--state").Run() == nil
}

func ufwAllow(port int, proto string) error {
	return run("ufw", "allow", fmt.Sprintf("%d/%s", port, proto))
}

func firewalldAllow(port int, proto string) error {
	if err := run("firewall-cmd", "--permanent", "--add-port", fmt.Sprintf("%d/%s", port, proto)); err != nil {
		return err
	}
	return run("firewall-cmd", "--reload")
}

func iptablesAllow(port int, proto string) error {
	// -C returns exit 0 if an identical rule already exists; skip the append
	// in that case so re-runs don't stack duplicate ACCEPT rules.
	check := exec.Command("iptables", "-C", "INPUT", "-p", proto, "--dport", fmt.Sprintf("%d", port), "-j", "ACCEPT")
	if check.Run() == nil {
		return nil
	}
	if err := run("iptables", "-A", "INPUT", "-p", proto, "--dport", fmt.Sprintf("%d", port), "-j", "ACCEPT"); err != nil {
		return err
	}
	// Best-effort: persist so the rule survives reboot. The runtime rule is
	// already in effect either way, so we don't surface persistence failures.
	_ = iptablesPersist()
	return nil
}

// iptablesPersist saves the current iptables ruleset and ensures a oneshot
// systemd unit re-applies it on boot. Idempotent.
func iptablesPersist() error {
	restorePath, err := exec.LookPath("iptables-restore")
	if err != nil {
		return err
	}

	out, err := exec.Command("iptables-save").Output()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(iptablesRulesPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(iptablesRulesPath, out, 0644); err != nil {
		return err
	}

	unit := fmt.Sprintf(iptablesUnitTemplate, restorePath, iptablesRulesPath)
	existing, _ := os.ReadFile(iptablesUnitPath)
	if string(existing) != unit {
		if err := os.WriteFile(iptablesUnitPath, []byte(unit), 0644); err != nil {
			return err
		}
		if err := run("systemctl", "daemon-reload"); err != nil {
			return err
		}
		if err := run("systemctl", "enable", "slipgate-iptables.service"); err != nil {
			return err
		}
	}
	return nil
}

// DisableResolvedStub disables systemd-resolved's DNS stub listener on port 53
// so that slipgate's DNS router can bind to it.
func DisableResolvedStub() error {
	// Check if systemd-resolved is running
	if err := exec.Command("systemctl", "is-active", "systemd-resolved").Run(); err != nil {
		return nil // not running, nothing to do
	}

	// Create drop-in config to disable stub listener
	if err := os.MkdirAll("/etc/systemd/resolved.conf.d", 0755); err != nil {
		return fmt.Errorf("create resolved conf dir: %w", err)
	}

	conf := "[Resolve]\nDNSStubListener=no\n"
	if err := os.WriteFile("/etc/systemd/resolved.conf.d/slipgate-no-stub.conf", []byte(conf), 0644); err != nil {
		return fmt.Errorf("write resolved config: %w", err)
	}

	// Restart systemd-resolved — after this, /etc/resolv.conf (whether a
	// symlink to /run/systemd/resolve/resolv.conf or a static file
	// managed elsewhere) will list the uplink DNS servers directly
	// rather than the stub at 127.0.0.53. We intentionally do NOT write
	// /etc/resolv.conf here: on most distros it's a symlink into
	// systemd's runtime directory, so os.WriteFile follows the link and
	// the target gets regenerated on the next network event. If you want
	// public resolvers (for WARP compatibility), see
	// warp.ensurePublicResolvers which sets them via resolved drop-in.
	if err := run("systemctl", "restart", "systemd-resolved"); err != nil {
		return fmt.Errorf("restart systemd-resolved: %w", err)
	}

	return nil
}

// FreePort kills any process listening on the given port/protocol.
// Uses fuser if available, falls back to lsof + kill.
func FreePort(port int, proto string) error {
	if _, err := exec.LookPath("fuser"); err == nil {
		// fuser -k sends SIGKILL to all processes using the port
		protoFlag := fmt.Sprintf("%d/%s", port, proto)
		_ = exec.Command("fuser", "-k", protoFlag).Run()
		return nil
	}

	// Fallback: ss to find PIDs, then kill
	out, err := exec.Command("ss", "-tlnp", fmt.Sprintf("sport = :%d", port)).Output()
	if err != nil {
		return nil // can't determine, skip
	}
	// Parse PIDs from ss output (format: pid=1234)
	for _, line := range splitLines(string(out)) {
		for _, field := range splitFields(line) {
			if len(field) > 4 && field[:4] == "pid=" {
				pid := field[4:]
				// strip trailing comma or paren
				for len(pid) > 0 && (pid[len(pid)-1] == ',' || pid[len(pid)-1] == ')') {
					pid = pid[:len(pid)-1]
				}
				_ = exec.Command("kill", "-9", pid).Run()
			}
		}
	}
	return nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func splitFields(s string) []string {
	var fields []string
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		start := i
		for i < len(s) && s[i] != ' ' && s[i] != '\t' {
			i++
		}
		if start < i {
			fields = append(fields, s[start:i])
		}
	}
	return fields
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}
