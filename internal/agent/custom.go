package agent

import (
	"fmt"
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

// ValidateDataPath checks that a data path is safe for use as a relative path
// under the home directory. It rejects absolute paths, path traversal, and
// empty strings.
func ValidateDataPath(path string) error {
	if path == "" {
		return fmt.Errorf("data path cannot be empty")
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("data path must be relative, got absolute: %q", path)
	}
	cleaned := filepath.Clean(path)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("data path contains traversal: %q", path)
	}
	return nil
}

// filterValidDataPaths returns only the paths that pass ValidateDataPath.
// Invalid paths are logged to stderr with a warning.
func filterValidDataPaths(paths []string, agentName, kind string) []string {
	var valid []string
	for _, p := range paths {
		if err := ValidateDataPath(p); err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: warning: skipping invalid %s for agent %q: %v\n", kind, agentName, err)
			continue
		}
		valid = append(valid, p)
	}
	return valid
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
			DataDirs:    filterValidDataPaths(f.AgentDef.DataDirs, agentName, "data_dir"),
			DataFiles:   filterValidDataPaths(f.AgentDef.DataFiles, agentName, "data_file"),
		}
	}

	if len(result) == 0 {
		return nil
	}

	return result
}
