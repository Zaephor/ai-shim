package agent

import "sort"

// Definition describes a built-in coding agent and how to install it.
type Definition struct {
	Name        string
	InstallType string
	Package     string
	Binary      string
	HomePaths   []string
}

var builtins = map[string]Definition{
	"claude-code": {Name: "claude-code", InstallType: "custom", Package: "curl -fsSL https://claude.ai/install.sh | bash", Binary: "claude", HomePaths: []string{".claude", ".claude.json"}},
	"gemini-cli":  {Name: "gemini-cli", InstallType: "npm", Package: "@google/gemini-cli", Binary: "gemini", HomePaths: []string{".gemini"}},
	"qwen-code":   {Name: "qwen-code", InstallType: "npm", Package: "@qwen-code/qwen-code", Binary: "qwen", HomePaths: []string{".qwen"}},
	"codex":       {Name: "codex", InstallType: "npm", Package: "@openai/codex", Binary: "codex", HomePaths: []string{".codex"}},
	"pi":          {Name: "pi", InstallType: "npm", Package: "@mariozechner/pi-coding-agent", Binary: "pi", HomePaths: []string{".pi"}},
	"gsd":         {Name: "gsd", InstallType: "npm", Package: "gsd-pi", Binary: "gsd", HomePaths: []string{".gsd"}},
	"aider":       {Name: "aider", InstallType: "uv", Package: "aider-chat", Binary: "aider", HomePaths: []string{".aider"}},
	"goose":       {Name: "goose", InstallType: "custom", Package: "curl -fsSL https://github.com/block/goose/releases/download/stable/download_cli.sh | bash", Binary: "goose", HomePaths: []string{".config/goose"}},
	"opencode":    {Name: "opencode", InstallType: "npm", Package: "opencode-ai", Binary: "opencode", HomePaths: []string{".config/opencode"}},
}

// Lookup returns the built-in agent definition for the given name.
func Lookup(name string) (Definition, bool) {
	def, ok := builtins[name]
	return def, ok
}

// All returns a copy of all built-in agent definitions.
func All() map[string]Definition {
	result := make(map[string]Definition, len(builtins))
	for k, v := range builtins {
		result[k] = v
	}
	return result
}

// Names returns a sorted list of all built-in agent names.
func Names() []string {
	names := make([]string, 0, len(builtins))
	for k := range builtins {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
