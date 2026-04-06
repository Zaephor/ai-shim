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

	fmt.Fprintf(&b, "\nexec %s", shell.Quote(p.Binary))
	for _, arg := range p.AgentArgs {
		fmt.Fprintf(&b, " %s", shell.Quote(arg))
	}
	b.WriteString("\n")

	return b.String()
}

// generateInstallCheck generates the shell logic that decides whether to
// install/reinstall an agent based on version pinning and update intervals.
// It sets need_install=true if installation should proceed.
func generateInstallCheck(p EntrypointParams) string {
	agentDir := shell.Quote(fmt.Sprintf("/usr/local/share/ai-shim/agents/%s", p.AgentName))
	binary := shell.Quote(p.Binary)
	pkg := shell.Quote(p.Package)

	var b strings.Builder

	fmt.Fprintf(&b, "LAST_UPDATE=\"%s/cache/.last-update\"\n", agentDir)
	fmt.Fprintf(&b, "INSTALLED_VERSION=\"%s/cache/.installed-version\"\n", agentDir)
	b.WriteString("need_install=false\n\n")

	if p.Version != "" {
		// Pinned version: compare installed version to requested
		fmt.Fprintf(&b, "# Pinned version check\n")
		fmt.Fprintf(&b, "if [ -f \"$INSTALLED_VERSION\" ] && [ \"$(cat \"$INSTALLED_VERSION\")\" = %s ]; then\n", shell.Quote(p.Version))
		fmt.Fprintf(&b, "  echo \"%s pinned at %s, already installed\"\n", pkg, shell.Quote(p.Version))
		b.WriteString("else\n")
		b.WriteString("  need_install=true\n")
		b.WriteString("fi\n")
	} else {
		// Unpinned: use update interval logic
		b.WriteString("# Check if binary exists\n")
		fmt.Fprintf(&b, "if ! command -v %s >/dev/null 2>&1; then\n", binary)
		b.WriteString("  need_install=true\n")

		switch {
		case p.UpdateInterval == 0:
			b.WriteString("else\n")
			b.WriteString("  # update_interval=always: reinstall every launch\n")
			b.WriteString("  need_install=true\n")
			b.WriteString("fi\n")
		case p.UpdateInterval < 0:
			b.WriteString("else\n")
			fmt.Fprintf(&b, "  echo \"%s already installed, skipping (update_interval=never)\"\n", pkg)
			b.WriteString("fi\n")
		default:
			b.WriteString("elif [ ! -f \"$LAST_UPDATE\" ]; then\n")
			b.WriteString("  need_install=true\n")
			b.WriteString("else\n")
			b.WriteString("  last=$(cat \"$LAST_UPDATE\")\n")
			b.WriteString("  now=$(date +%s)\n")
			b.WriteString("  elapsed=$((now - last))\n")
			// If elapsed is negative the host clock moved backwards (or
			// LAST_UPDATE was written from a future-clock host). Treat
			// the cache as stale and reinstall.
			fmt.Fprintf(&b, "  if [ \"$elapsed\" -lt 0 ] || [ \"$elapsed\" -ge %d ]; then\n", p.UpdateInterval)
			fmt.Fprintf(&b, "    echo \"Update interval elapsed, reinstalling %s...\"\n", pkg)
			b.WriteString("    need_install=true\n")
			b.WriteString("  else\n")
			fmt.Fprintf(&b, "    echo \"%s is up to date (checked $((elapsed / 60))m ago)\"\n", pkg)
			b.WriteString("  fi\n")
			b.WriteString("fi\n")
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
	var b strings.Builder
	b.WriteString("  date +%s > \"$LAST_UPDATE\"\n")
	fmt.Fprintf(&b, "  echo %s > \"$INSTALLED_VERSION\"\n", shell.Quote(version))
	return b.String()
}

func generateNPMInstall(p EntrypointParams) string {
	pkg := shell.Quote(p.Package)
	if p.Version != "" {
		pkg = shell.Quote(fmt.Sprintf("%s@%s", p.Package, p.Version))
	}
	agentDir := shell.Quote(fmt.Sprintf("/usr/local/share/ai-shim/agents/%s", p.AgentName))

	var b strings.Builder
	fmt.Fprintf(&b, "export NPM_CONFIG_PREFIX=%s/bin\n", agentDir)
	fmt.Fprintf(&b, "export NPM_CONFIG_CACHE=%s/cache\n", agentDir)
	b.WriteString("export PATH=\"$NPM_CONFIG_PREFIX/bin:$PATH\"\n")

	b.WriteString(generateInstallCheck(p))

	b.WriteString("if [ \"$need_install\" = true ]; then\n")
	fmt.Fprintf(&b, "  echo \"Installing %s via npm...\"\n", pkg)
	fmt.Fprintf(&b, "  npm install -g %s || { echo \"ERROR: npm install failed for %s\"; exit 1; }\n", pkg, pkg)
	b.WriteString(generatePostInstall(p))
	b.WriteString("fi\n")

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
	// Bootstrap uv if not already installed
	b.WriteString("if ! command -v uv >/dev/null 2>&1; then\n")
	b.WriteString("  echo \"Installing uv...\"\n")
	b.WriteString("  curl -LsSf https://astral.sh/uv/install.sh | sh\n")
	b.WriteString("  export PATH=\"$HOME/.local/bin:$PATH\"\n")
	b.WriteString("fi\n")
	fmt.Fprintf(&b, "export UV_TOOL_DIR=%s/bin/tools\n", agentDir)
	fmt.Fprintf(&b, "export UV_TOOL_BIN_DIR=%s/bin/bin\n", agentDir)
	b.WriteString("export PATH=\"$UV_TOOL_BIN_DIR:$PATH\"\n")

	b.WriteString(generateInstallCheck(p))

	b.WriteString("if [ \"$need_install\" = true ]; then\n")
	fmt.Fprintf(&b, "  echo \"Installing %s via uv...\"\n", pkg)
	fmt.Fprintf(&b, "  uv tool install %s || uv tool upgrade %s || { echo \"ERROR: uv install failed for %s\"; exit 1; }\n", pkg, basePkg, basePkg)
	b.WriteString(generatePostInstall(p))
	b.WriteString("fi\n")

	return b.String()
}

func generateCustomInstall(p EntrypointParams) string {
	var b strings.Builder
	// Custom installers often put binaries in user-local paths.
	// Extend PATH so the exec line can find them.
	b.WriteString("export PATH=\"$HOME/.local/bin:$HOME/.cargo/bin:$PATH\"\n")
	// Use set +e during install so post-install steps (config, telemetry)
	// can fail without preventing the binary from being installed.
	b.WriteString("set +e\n")
	b.WriteString(p.Package + "\n")
	b.WriteString("set -e\n")
	return b.String()
}
