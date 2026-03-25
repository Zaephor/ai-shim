package install

import (
	"fmt"
	"strings"
)

// EntrypointParams holds the parameters for generating a container entrypoint script.
type EntrypointParams struct {
	InstallType string
	Package     string
	Binary      string
	Version     string
	AgentArgs   []string
}

// GenerateEntrypoint produces a shell script that installs and execs the agent binary.
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
	default:
		b.WriteString(fmt.Sprintf("echo \"ERROR: unknown install type: %s\"\nexit 1\n", p.InstallType))
	}

	b.WriteString(fmt.Sprintf("\nexec %s", p.Binary))
	for _, arg := range p.AgentArgs {
		b.WriteString(fmt.Sprintf(" %s", shellQuote(arg)))
	}
	b.WriteString("\n")

	return b.String()
}

func generateNPMInstall(p EntrypointParams) string {
	pkg := p.Package
	if p.Version != "" {
		pkg = fmt.Sprintf("%s@%s", p.Package, p.Version)
	}
	return fmt.Sprintf("npm install -g %s 2>/dev/null\n", pkg)
}

func generateUVInstall(p EntrypointParams) string {
	pkg := p.Package
	if p.Version != "" {
		pkg = fmt.Sprintf("%s==%s", p.Package, p.Version)
	}
	return fmt.Sprintf("uv tool install %s 2>/dev/null || uv tool upgrade %s 2>/dev/null\n", pkg, p.Package)
}

func generateCustomInstall(p EntrypointParams) string {
	return p.Package + "\n"
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'\\$`!#&|;(){}[]<>?*~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
