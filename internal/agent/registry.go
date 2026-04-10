package agent

import (
	"fmt"
	"os"
	"sort"
	"sync"
)

// Definition describes a built-in coding agent and how to install it.
type Definition struct {
	Name        string
	InstallType string
	Package     string
	Binary      string
	DataDirs    []string // directories under ~/ to persist (e.g. ".claude", ".config/goose")
	DataFiles   []string // files under ~/ to persist (e.g. ".claude.json")
}

var builtins = map[string]Definition{
	"claude-code": {Name: "claude-code", InstallType: "custom", Package: "curl -fsSL https://claude.ai/install.sh | bash", Binary: "claude", DataDirs: []string{".claude"}, DataFiles: []string{".claude.json"}},
	"gemini-cli":  {Name: "gemini-cli", InstallType: "npm", Package: "@google/gemini-cli", Binary: "gemini", DataDirs: []string{".gemini"}},
	"qwen-code":   {Name: "qwen-code", InstallType: "npm", Package: "@qwen-code/qwen-code", Binary: "qwen", DataDirs: []string{".qwen"}},
	"codex":       {Name: "codex", InstallType: "npm", Package: "@openai/codex", Binary: "codex", DataDirs: []string{".codex"}},
	"copilot-cli": {Name: "copilot-cli", InstallType: "npm", Package: "@github/copilot", Binary: "copilot", DataDirs: []string{".copilot"}},
	"pi":          {Name: "pi", InstallType: "npm", Package: "@mariozechner/pi-coding-agent", Binary: "pi", DataDirs: []string{".pi"}},
	"gsd":         {Name: "gsd", InstallType: "npm", Package: "gsd-pi", Binary: "gsd", DataDirs: []string{".gsd"}},
	"aider":       {Name: "aider", InstallType: "uv", Package: "aider-chat", Binary: "aider", DataDirs: []string{".aider"}},
	"goose":       {Name: "goose", InstallType: "custom", Package: "curl -fsSL https://github.com/block/goose/releases/download/stable/download_cli.sh | bash", Binary: "goose", DataDirs: []string{".config/goose"}},
	"opencode":    {Name: "opencode", InstallType: "npm", Package: "opencode-ai", Binary: "opencode", DataDirs: []string{".config/opencode"}},
}

func init() {
	// Validate built-in agent data paths at startup (belt and suspenders).
	for name, def := range builtins {
		for _, dir := range def.DataDirs {
			if err := ValidateDataPath(dir); err != nil {
				fmt.Fprintf(os.Stderr, "ai-shim: BUG: built-in agent %q has invalid data_dir: %v\n", name, err)
			}
		}
		for _, file := range def.DataFiles {
			if err := ValidateDataPath(file); err != nil {
				fmt.Fprintf(os.Stderr, "ai-shim: BUG: built-in agent %q has invalid data_file: %v\n", name, err)
			}
		}
	}
}

// customs holds user-defined agent definitions loaded from config.
// Custom agents override built-ins if they share the same name.
var (
	customs   = map[string]Definition{}
	customsMu sync.RWMutex
)

// SetCustomAgents registers user-defined agents. Custom agents override
// built-in agents when looked up by name.
func SetCustomAgents(defs map[string]Definition) {
	customsMu.Lock()
	defer customsMu.Unlock()
	if defs == nil {
		customs = map[string]Definition{}
		return
	}
	customs = defs
}

// Lookup returns the agent definition for the given name. Custom agents
// are checked first, then built-ins.
func Lookup(name string) (Definition, bool) {
	customsMu.RLock()
	defer customsMu.RUnlock()
	if def, ok := customs[name]; ok {
		return def, true
	}
	def, ok := builtins[name]
	return def, ok
}

// All returns a copy of all agent definitions (built-in + custom).
// Custom agents override built-ins with the same name.
func All() map[string]Definition {
	customsMu.RLock()
	defer customsMu.RUnlock()
	result := make(map[string]Definition, len(builtins)+len(customs))
	for k, v := range builtins {
		result[k] = v
	}
	for k, v := range customs {
		result[k] = v
	}
	return result
}

// Names returns a sorted list of all agent names (built-in + custom).
func Names() []string {
	all := All()
	names := make([]string, 0, len(all))
	for k := range all {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
