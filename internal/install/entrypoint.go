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
	return fmt.Sprintf("echo \"Installing %s via npm...\"\nnpm install -g %s || { echo \"ERROR: npm install failed for %s\"; exit 1; }\n", pkg, pkg, pkg)
}

func generateUVInstall(p EntrypointParams) string {
	pkg := shell.Quote(p.Package)
	if p.Version != "" {
		pkg = shell.Quote(fmt.Sprintf("%s==%s", p.Package, p.Version))
	}
	basePkg := shell.Quote(p.Package)
	return fmt.Sprintf("echo \"Installing %s via uv...\"\nuv tool install %s || uv tool upgrade %s || { echo \"ERROR: uv install failed for %s\"; exit 1; }\n", pkg, pkg, basePkg, basePkg)
}

func generateCustomInstall(p EntrypointParams) string {
	return p.Package + "\n"
}

