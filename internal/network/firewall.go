package network

import (
	"fmt"
	"strings"

	"github.com/ai-shim/ai-shim/internal/config"
)

// GenerateFirewallScript generates iptables commands for network egress rules.
// The script must run as root before dropping to the agent user.
// Returns an empty string if no rules are configured.
func GenerateFirewallScript(rules *config.NetworkRules) string {
	if rules == nil {
		return ""
	}
	if len(rules.AllowedHosts) == 0 && len(rules.BlockedHosts) == 0 && len(rules.AllowedPorts) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("# ai-shim network firewall rules\n")

	// Allow loopback and established connections
	b.WriteString("iptables -A OUTPUT -o lo -j ACCEPT\n")
	b.WriteString("iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT\n")

	if len(rules.AllowedHosts) > 0 {
		// Allowlist mode: allow only specified hosts, drop everything else
		for _, host := range rules.AllowedHosts {
			b.WriteString(fmt.Sprintf("iptables -A OUTPUT -d %s -j ACCEPT\n", host))
		}
		// Allow DNS so host resolution works
		b.WriteString("iptables -A OUTPUT -p udp --dport 53 -j ACCEPT\n")
		b.WriteString("iptables -A OUTPUT -p tcp --dport 53 -j ACCEPT\n")
		b.WriteString("iptables -A OUTPUT -j DROP\n")
	} else if len(rules.BlockedHosts) > 0 {
		// Blocklist mode: block specified hosts, allow everything else
		for _, host := range rules.BlockedHosts {
			b.WriteString(fmt.Sprintf("iptables -A OUTPUT -d %s -j DROP\n", host))
		}
	}

	if len(rules.AllowedPorts) > 0 {
		// Port restrictions: only allow specified ports
		// Insert before any final DROP rule from allowlist mode
		for _, port := range rules.AllowedPorts {
			b.WriteString(fmt.Sprintf("iptables -A OUTPUT -p tcp --dport %s -j ACCEPT\n", port))
			b.WriteString(fmt.Sprintf("iptables -A OUTPUT -p udp --dport %s -j ACCEPT\n", port))
		}
		// If not in allowlist mode (which already has a DROP), add one for ports
		if len(rules.AllowedHosts) == 0 {
			b.WriteString("iptables -A OUTPUT -p tcp -j DROP\n")
			b.WriteString("iptables -A OUTPUT -p udp -j DROP\n")
		}
	}

	return b.String()
}

