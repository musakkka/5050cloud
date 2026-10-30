// SPDX-License-Identifier: MIT

// Package install holds only tests: the installer is a shell script, and these
// read the unit files and firewall rules it writes.
package install

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func script(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// between returns the text from start up to the next end after it.
func between(t *testing.T, s, start, end string) string {
	t.Helper()
	_, rest, ok := strings.Cut(s, start)
	if !ok {
		t.Fatalf("%q is not in the installer", start)
	}
	out, _, ok := strings.Cut(rest, end)
	if !ok {
		t.Fatalf("%q has no %q after it", start, end)
	}
	return out
}

// units returns every systemd unit the installer can write, by name.
func units(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	files, err := filepath.Glob("systemd/*.service")
	if err != nil || len(files) == 0 {
		t.Fatalf("no unit files: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.Base(f)] = string(b)
	}
	re := regexp.MustCompile(`(?s)cat >"\$UNIT_DIR/([^"]+\.service)" <<'?EOF'?\n(.*?)\nEOF\n`)
	for _, m := range re.FindAllStringSubmatch(script(t), -1) {
		out[m[1]] = m[2]
	}
	if _, ok := out["5050-firewall.service"]; !ok {
		t.Fatal("the firewall unit was not found")
	}
	if _, ok := out["5050-tunnel.service"]; !ok {
		t.Fatal("the tunnel unit was not found")
	}
	return out
}

// A unit with default dependencies runs after sysinit.target and basic.target,
// so it cannot also come before anything that sysinit.target waits for.
func TestNoUnitCanFormAnOrderingCycle(t *testing.T) {
	early := regexp.MustCompile(`(?m)^Before=.*\b(network-pre|sysinit|basic|local-fs|cloud-init)`)
	for name, text := range units(t) {
		if early.MatchString(text) && !strings.Contains(text, "DefaultDependencies=no") {
			t.Errorf("%s is ordered before early boot but keeps default dependencies:\n%s", name, text)
		}
	}
}

func TestFirewallUnitIsEarlyAndHasNoAfter(t *testing.T) {
	u := units(t)["5050-firewall.service"]
	for _, want := range []string{"DefaultDependencies=no", "Before=network-pre.target shutdown.target", "Conflicts=shutdown.target", "WantedBy=sysinit.target"} {
		if !strings.Contains(u, want) {
			t.Errorf("the firewall unit has no %q:\n%s", want, u)
		}
	}
	if strings.Contains(u, "After=") || strings.Contains(u, "multi-user.target") {
		t.Errorf("the firewall unit must have no After= and not wait for multi-user:\n%s", u)
	}
}

func TestPrivateRulesCloseContainersFromTheHost(t *testing.T) {
	conf := between(t, script(t), "private_firewall_conf() {", "\n}\n")
	input := between(t, conf, "chain input {", "}\n\tchain forward")
	forward := between(t, conf, "chain forward {", "\n\t}")

	for _, bad := range []string{`iifname "docker0" accept`, `iifname "br-*" accept`, `iifname "f5-*" accept`} {
		if strings.Contains(input, bad) {
			t.Errorf("the input chain still has %s", bad)
		}
	}
	for _, want := range []string{`iifname "docker0" drop`, `iifname "br-*" drop`, `iifname "f5-*" drop`, `tcp dport { 53, 5000 } accept`} {
		if !strings.Contains(input, want) {
			t.Errorf("the input chain has no %s", want)
		}
	}
	// The drops come first: they must not depend on an established flow.
	ip6 := strings.Index(forward, "meta nfproto ipv6 drop")
	meta := strings.Index(forward, "ip daddr 169.254.169.254 ip saddr != @metadata_allow drop")
	wire := strings.Index(forward, "ip daddr 168.63.129.16 meta l4proto { tcp, udp } th dport != 53 ip saddr != @metadata_allow drop")
	est := strings.Index(forward, "ct state established")
	if ip6 < 0 || meta < 0 || wire < 0 || est < 0 || ip6 > est || meta > est || wire > est {
		t.Errorf("the forward chain must drop IPv6 and metadata before anything else:\n%s", forward)
	}
}

func TestSSHIsMaskedAndOpenedAgain(t *testing.T) {
	s := script(t)
	if !strings.Contains(between(t, s, "ssh_close() {", "\n}\n"), `systemctl mask "$u"`) {
		t.Error("ssh_close does not mask the units")
	}
	open := between(t, s, "ssh_open() {", "\n}\n")
	if !strings.Contains(open, `systemctl unmask "$u"`) || !strings.Contains(open, "enable --now ssh.service") {
		t.Error("ssh_open does not unmask and start SSH")
	}
}

func TestRecoveryKeyIsNotOnACommandLine(t *testing.T) {
	if strings.Contains(script(t), `--recovery-key "$RECOVERY_KEY"`) {
		t.Error("the recovery key is passed as an argument")
	}
}
