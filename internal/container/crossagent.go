package container

import (
	"github.com/docker/docker/api/types/mount"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/storage"
)

// CrossAgentMounts generates additional mounts for cross-agent access.
// When isolated=false, all installed agents' bins are mounted.
// When isolated=true (default), only agents in allowAgents are mounted.
// Home paths are already shared via the profile home mount.
func CrossAgentMounts(layout storage.Layout, primaryAgent string, allowAgents []string, isolated bool) []mount.Mount {
	var mounts []mount.Mount

	agents := determineAccessibleAgents(primaryAgent, allowAgents, isolated)

	for _, agentName := range agents {
		if _, ok := agent.Lookup(agentName); !ok {
			continue
		}

		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: layout.AgentBin(agentName),
			Target: "/usr/local/share/ai-shim/agents/" + agentName + "/bin",
		})
	}

	return mounts
}

func determineAccessibleAgents(primaryAgent string, allowAgents []string, isolated bool) []string {
	if !isolated {
		// Non-isolated: all known agents except primary (primary is already mounted)
		all := agent.Names()
		var result []string
		for _, name := range all {
			if name != primaryAgent {
				result = append(result, name)
			}
		}
		return result
	}

	// Isolated: only explicitly allowed agents
	var result []string
	for _, name := range allowAgents {
		if name != primaryAgent {
			result = append(result, name)
		}
	}
	return result
}
