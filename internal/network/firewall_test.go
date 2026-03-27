package network

import (
	"testing"

	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestGenerateFirewallScript_Nil(t *testing.T) {
	assert.Empty(t, GenerateFirewallScript(nil))
}

func TestGenerateFirewallScript_EmptyRules(t *testing.T) {
	assert.Empty(t, GenerateFirewallScript(&config.NetworkRules{}))
}

func TestGenerateFirewallScript_AllowedHosts(t *testing.T) {
	rules := &config.NetworkRules{
		AllowedHosts: []string{"api.example.com", "10.0.0.1"},
	}
	script := GenerateFirewallScript(rules)

	assert.Contains(t, script, "iptables -A OUTPUT -o lo -j ACCEPT", "should allow loopback")
	assert.Contains(t, script, "iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT", "should allow established")
	assert.Contains(t, script, "iptables -A OUTPUT -d api.example.com -j ACCEPT")
	assert.Contains(t, script, "iptables -A OUTPUT -d 10.0.0.1 -j ACCEPT")
	assert.Contains(t, script, "iptables -A OUTPUT -p udp --dport 53 -j ACCEPT", "should allow DNS")
	assert.Contains(t, script, "iptables -A OUTPUT -j DROP", "should drop everything else")
}

func TestGenerateFirewallScript_BlockedHosts(t *testing.T) {
	rules := &config.NetworkRules{
		BlockedHosts: []string{"bad.example.com"},
	}
	script := GenerateFirewallScript(rules)

	assert.Contains(t, script, "iptables -A OUTPUT -d bad.example.com -j DROP")
	assert.NotContains(t, script, "iptables -A OUTPUT -j DROP", "should not have blanket drop in blocklist mode")
}

func TestGenerateFirewallScript_AllowedPorts(t *testing.T) {
	rules := &config.NetworkRules{
		AllowedPorts: []string{"443", "80"},
	}
	script := GenerateFirewallScript(rules)

	assert.Contains(t, script, "iptables -A OUTPUT -p tcp --dport 443 -j ACCEPT")
	assert.Contains(t, script, "iptables -A OUTPUT -p udp --dport 443 -j ACCEPT")
	assert.Contains(t, script, "iptables -A OUTPUT -p tcp --dport 80 -j ACCEPT")
	assert.Contains(t, script, "iptables -A OUTPUT -p tcp -j DROP", "should drop other TCP")
	assert.Contains(t, script, "iptables -A OUTPUT -p udp -j DROP", "should drop other UDP")
}

func TestGenerateFirewallScript_AllowedHostsAndPorts(t *testing.T) {
	rules := &config.NetworkRules{
		AllowedHosts: []string{"api.example.com"},
		AllowedPorts: []string{"443"},
	}
	script := GenerateFirewallScript(rules)

	// In allowlist mode with ports, the blanket DROP from allowlist covers port restriction too
	assert.Contains(t, script, "iptables -A OUTPUT -d api.example.com -j ACCEPT")
	assert.Contains(t, script, "iptables -A OUTPUT -p tcp --dport 443 -j ACCEPT")
	assert.Contains(t, script, "iptables -A OUTPUT -j DROP")
	// Should NOT have the per-protocol DROP since allowlist already has blanket DROP
	assert.NotContains(t, script, "iptables -A OUTPUT -p tcp -j DROP")
}

