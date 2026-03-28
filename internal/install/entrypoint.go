package install

import (
	"fmt"
	"strings"

	"github.com/ai-shim/ai-shim/internal/shell"
)

type EntrypointParams struct {
	InstallType string
	Package     string
	Binary      string
	Version     string
	AgentArgs   []string
	AgentName   string // used to resolve persistent install paths
}

func GenerateEntrypoint(p EntrypointParams) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\nset -e\n\n")

	switch p.InstallType {
	case "npm":
		b.WriteString(generateNPMInstall(p))
	case "uv":
		b.WriteString(generateUVInstall(p))
	case "custom":
		b.WriteString(generateCustomInstall(p))
	}

	b.WriteString(fmt.Sprintf("\nexec %s", shell.Quote(p.Binary)))
	for _, arg := range p.AgentArgs {
		b.WriteString(fmt.Sprintf(" %s", shell.Quote(arg)))
	}
	b.WriteString("\n")

	return b.String()
}

func generateNPMInstall(p EntrypointParams) string {
	pkg := shell.Quote(p.Package)
	if p.Version != "" {
		pkg = shell.Quote(fmt.Sprintf("%s@%s", p.Package, p.Version))
	}
	agentDir := shell.Quote(fmt.Sprintf("/usr/local/share/ai-shim/agents/%s", p.AgentName))
	// Install to the persistent agent directory so subsequent launches
	// skip the download. The bin/ and cache/ subdirectories are bind-mounted
	// from the host, surviving container removal.
	return fmt.Sprintf(`export NPM_CONFIG_PREFIX=%s/bin
export NPM_CONFIG_CACHE=%s/cache
export PATH="$NPM_CONFIG_PREFIX/bin:$PATH"
if command -v %s >/dev/null 2>&1; then
  echo "%s already installed, skipping download"
else
  echo "Installing %s via npm..."
  npm install -g %s || { echo "ERROR: npm install failed for %s"; exit 1; }
fi
`, agentDir, agentDir, shell.Quote(p.Binary), pkg, pkg, pkg, pkg)
}

func generateUVInstall(p EntrypointParams) string {
	pkg := shell.Quote(p.Package)
	if p.Version != "" {
		pkg = shell.Quote(fmt.Sprintf("%s==%s", p.Package, p.Version))
	}
	basePkg := shell.Quote(p.Package)
	agentDir := shell.Quote(fmt.Sprintf("/usr/local/share/ai-shim/agents/%s", p.AgentName))
	// Install to the persistent agent directory so subsequent launches
	// skip the download.
	return fmt.Sprintf(`export UV_TOOL_DIR=%s/bin/tools
export UV_TOOL_BIN_DIR=%s/bin/bin
export PATH="$UV_TOOL_BIN_DIR:$PATH"
if command -v %s >/dev/null 2>&1; then
  echo "%s already installed, skipping download"
else
  echo "Installing %s via uv..."
  uv tool install %s || uv tool upgrade %s || { echo "ERROR: uv install failed for %s"; exit 1; }
fi
`, agentDir, agentDir, shell.Quote(p.Binary), pkg, pkg, pkg, basePkg, basePkg)
}

func generateCustomInstall(p EntrypointParams) string {
	return p.Package + "\n"
}
