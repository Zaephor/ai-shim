package agent

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// customAgentFile is the YAML structure for an agent config file that may
// optionally contain an agent_def block defining a custom agent.
type customAgentFile struct {
	AgentDef *customAgentDef `yaml:"agent_def,omitempty"`
}

// customAgentDef mirrors the Definition struct for YAML unmarshalling.
type customAgentDef struct {
	InstallType string   `yaml:"install_type"`
	Package     string   `yaml:"package"`
	Binary      string   `yaml:"binary"`
	DataDirs    []string `yaml:"data_dirs,omitempty"`
	DataFiles   []string `yaml:"data_files,omitempty"`
}

// LoadCustomAgents scans configDir/agents/*.yaml for files containing an
// agent_def block. Each matching file produces a Definition keyed by the
// filename (without .yaml extension). Custom agents override built-in agents
// if they share the same name.
func LoadCustomAgents(configDir string) map[string]Definition {
	agentsDir := filepath.Join(configDir, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil
	}

	result := make(map[string]Definition)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(agentsDir, name))
		if err != nil {
			continue
		}
		if len(data) == 0 {
			continue
		}

		var f customAgentFile
		if err := yaml.Unmarshal(data, &f); err != nil {
			continue
		}
		if f.AgentDef == nil {
			continue
		}

		agentName := strings.TrimSuffix(name, ".yaml")
		result[agentName] = Definition{
			Name:        agentName,
			InstallType: f.AgentDef.InstallType,
			Package:     f.AgentDef.Package,
			Binary:      f.AgentDef.Binary,
			DataDirs:    f.AgentDef.DataDirs,
			DataFiles:   f.AgentDef.DataFiles,
		}
	}

	if len(result) == 0 {
		return nil
	}

	return result
}
