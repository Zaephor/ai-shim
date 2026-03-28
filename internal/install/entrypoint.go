package install

import (
	"fmt"
	"strings"

	"github.com/ai-shim/ai-shim/internal/shell"
)

type EntrypointParams struct {
	InstallType    string
	Package        string
	Binary         string
	Version        string
	AgentArgs      []string
	AgentName      string // used to resolve persistent install paths
	UpdateInterval int64  // seconds: 0=always, -1=never, >0=interval
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

// generateInstallCheck generates the shell logic that decides whether to
// install/reinstall an agent based on version pinning and update intervals.
// It sets need_install=true if installation should proceed.
func generateInstallCheck(p EntrypointParams) string {
	agentDir := fmt.Sprintf("/usr/local/share/ai-shim/agents/%s", p.AgentName)
	binary := shell.Quote(p.Binary)
	pkg := shell.Quote(p.Package)

	var b strings.Builder

	b.WriteString(fmt.Sprintf(`LAST_UPDATE="%s/cache/.last-update"
INSTALLED_VERSION="%s/cache/.installed-version"
need_install=false

`, agentDir, agentDir))

	if p.Version != "" {
		// Pinned version: compare installed version to requested
		b.WriteString(fmt.Sprintf(`# Pinned version check
if [ -f "$INSTALLED_VERSION" ] && [ "$(cat "$INSTALLED_VERSION")" = %s ]; then
  echo "%s pinned at %s, already installed"
else
  need_install=true
fi
`, shell.Quote(p.Version), pkg, shell.Quote(p.Version)))
	} else {
		// Unpinned: use update interval logic
		b.WriteString(fmt.Sprintf(`# Check if binary exists
if ! command -v %s >/dev/null 2>&1; then
  need_install=true
`, binary))

		switch {
		case p.UpdateInterval == 0:
			// Always reinstall
			b.WriteString(`else
  # update_interval=always: reinstall every launch
  need_install=true
fi
`)
		case p.UpdateInterval < 0:
			// Never reinstall
			b.WriteString(fmt.Sprintf(`else
  echo "%s already installed, skipping (update_interval=never)"
fi
`, pkg))
		default:
			// Interval-based
			b.WriteString(fmt.Sprintf(`elif [ ! -f "$LAST_UPDATE" ]; then
  need_install=true
else
  last=$(cat "$LAST_UPDATE")
  now=$(date +%%s)
  elapsed=$((now - last))
  if [ "$elapsed" -ge %d ]; then
    echo "Update interval elapsed, reinstalling %s..."
    need_install=true
  else
    echo "%s is up to date (checked $((elapsed / 60))m ago)"
  fi
fi
`, p.UpdateInterval, pkg, pkg))
		}
	}

	return b.String()
}

// generatePostInstall generates the shell logic to record installation metadata.
func generatePostInstall(p EntrypointParams) string {
	version := "latest"
	if p.Version != "" {
		version = p.Version
	}
	return fmt.Sprintf(`  date +%%s > "$LAST_UPDATE"
  echo %s > "$INSTALLED_VERSION"
`, shell.Quote(version))
}

func generateNPMInstall(p EntrypointParams) string {
	pkg := shell.Quote(p.Package)
	if p.Version != "" {
		pkg = shell.Quote(fmt.Sprintf("%s@%s", p.Package, p.Version))
	}
	agentDir := shell.Quote(fmt.Sprintf("/usr/local/share/ai-shim/agents/%s", p.AgentName))

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`export NPM_CONFIG_PREFIX=%s/bin
export NPM_CONFIG_CACHE=%s/cache
export PATH="$NPM_CONFIG_PREFIX/bin:$PATH"
`, agentDir, agentDir))

	b.WriteString(generateInstallCheck(p))

	b.WriteString(fmt.Sprintf(`if [ "$need_install" = true ]; then
  echo "Installing %s via npm..."
  npm install -g %s || { echo "ERROR: npm install failed for %s"; exit 1; }
%sfi
`, pkg, pkg, pkg, generatePostInstall(p)))

	return b.String()
}

func generateUVInstall(p EntrypointParams) string {
	pkg := shell.Quote(p.Package)
	if p.Version != "" {
		pkg = shell.Quote(fmt.Sprintf("%s==%s", p.Package, p.Version))
	}
	basePkg := shell.Quote(p.Package)
	agentDir := shell.Quote(fmt.Sprintf("/usr/local/share/ai-shim/agents/%s", p.AgentName))

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`export UV_TOOL_DIR=%s/bin/tools
export UV_TOOL_BIN_DIR=%s/bin/bin
export PATH="$UV_TOOL_BIN_DIR:$PATH"
`, agentDir, agentDir))

	b.WriteString(generateInstallCheck(p))

	b.WriteString(fmt.Sprintf(`if [ "$need_install" = true ]; then
  echo "Installing %s via uv..."
  uv tool install %s || uv tool upgrade %s || { echo "ERROR: uv install failed for %s"; exit 1; }
%sfi
`, pkg, pkg, basePkg, basePkg, generatePostInstall(p)))

	return b.String()
}

func generateCustomInstall(p EntrypointParams) string {
	return p.Package + "\n"
}
