package container

import (
	"path/filepath"

	"github.com/docker/docker/api/types/mount"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/storage"
)

// CrossAgentMounts generates additional mounts for cross-agent access.
// When isolated=false, all installed agents' home paths and bins are mounted.
// When isolated=true (default), only agents in allowAgents are mounted.
func CrossAgentMounts(layout storage.Layout, primaryAgent string, allowAgents []string, isolated bool) []mount.Mount {
	var mounts []mount.Mount

	agents := determineAccessibleAgents(primaryAgent, allowAgents, isolated)

	for _, agentName := range agents {
		agentDef, ok := agent.Lookup(agentName)
		if !ok {
			continue
		}

		// Mount agent bin
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: layout.AgentBin(agentName),
			Target: "/opt/ai-shim/agents/" + agentName + "/bin",
		})

		// Mount agent home paths from the profile
		for _, homePath := range agentDef.HomePaths {
			// Home paths are relative (e.g. ".claude"), mount them in /home/user
			mounts = append(mounts, mount.Mount{
				Type:   mount.TypeBind,
				Source: filepath.Join(layout.Root, "agents", agentName, "home", homePath),
				Target: "/home/user/" + homePath,
			})
		}
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
