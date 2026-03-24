package provision

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// ToolDef mirrors config.ToolDef — the tool definition from config.
type ToolDef struct {
	Type     string   // binary-download, tar-extract, tar-extract-selective, apt, go-install, custom
	URL      string
	Binary   string
	Files    []string // additional files for tar-extract-selective
	Package  string   // for apt/go-install
	Install  string   // shell script for custom type
	Checksum string   // optional sha256 checksum
}

// GenerateInstallScript generates a shell script that provisions all tools.
// The script checks if each tool already exists (cache-aware) before downloading.
func GenerateInstallScript(tools map[string]ToolDef, targetDir string) string {
	if len(tools) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("# Tool provisioning\n")

	for name, tool := range tools {
		b.WriteString(fmt.Sprintf("\n# Install: %s\n", name))
		b.WriteString(generateToolInstall(tool, targetDir))
	}

	return b.String()
}

func generateToolInstall(tool ToolDef, targetDir string) string {
	var b strings.Builder

	switch tool.Type {
	case "binary-download":
		// Check if binary already exists
		b.WriteString(fmt.Sprintf("if [ ! -f \"%s/%s\" ]; then\n", targetDir, tool.Binary))
		b.WriteString(fmt.Sprintf("  curl -fsSL -o \"%s/%s\" \"%s\"\n", targetDir, tool.Binary, tool.URL))
		b.WriteString(fmt.Sprintf("  chmod +x \"%s/%s\"\n", targetDir, tool.Binary))
		if tool.Checksum != "" {
			b.WriteString(fmt.Sprintf("  echo \"%s  %s/%s\" | sha256sum -c -\n", tool.Checksum, targetDir, tool.Binary))
		}
		b.WriteString("fi\n")

	case "tar-extract":
		b.WriteString(fmt.Sprintf("if [ ! -f \"%s/%s\" ]; then\n", targetDir, tool.Binary))
		b.WriteString(fmt.Sprintf("  curl -fsSL \"%s\" | tar xz -C \"%s\" --strip-components=1 --wildcards '*/%s' 2>/dev/null || \\\n", tool.URL, targetDir, tool.Binary))
		b.WriteString(fmt.Sprintf("  curl -fsSL \"%s\" | tar xz -C /tmp && find /tmp -name \"%s\" -exec mv {} \"%s/\" \\;\n", tool.URL, tool.Binary, targetDir))
		b.WriteString(fmt.Sprintf("  chmod +x \"%s/%s\"\n", targetDir, tool.Binary))
		b.WriteString("fi\n")

	case "tar-extract-selective":
		b.WriteString(fmt.Sprintf("if [ ! -f \"%s/%s\" ]; then\n", targetDir, tool.Binary))
		files := append([]string{tool.Binary}, tool.Files...)
		wildcards := make([]string, len(files))
		for i, f := range files {
			wildcards[i] = fmt.Sprintf("'*/%s'", f)
		}
		b.WriteString(fmt.Sprintf("  curl -fsSL \"%s\" | tar xz -C \"%s\" --strip-components=1 --wildcards %s 2>/dev/null\n",
			tool.URL, targetDir, strings.Join(wildcards, " ")))
		b.WriteString(fmt.Sprintf("  chmod +x \"%s/%s\"\n", targetDir, tool.Binary))
		b.WriteString("fi\n")

	case "apt":
		b.WriteString(fmt.Sprintf("if ! command -v %s >/dev/null 2>&1; then\n", tool.Binary))
		b.WriteString(fmt.Sprintf("  apt-get update -qq && apt-get install -y -qq %s\n", tool.Package))
		b.WriteString("fi\n")

	case "go-install":
		b.WriteString(fmt.Sprintf("if [ ! -f \"%s/%s\" ]; then\n", targetDir, tool.Binary))
		b.WriteString(fmt.Sprintf("  GOBIN=\"%s\" go install %s@latest\n", targetDir, tool.Package))
		b.WriteString("fi\n")

	case "custom":
		b.WriteString(tool.Install + "\n")
	}

	return b.String()
}

// VerifyChecksum verifies a file's SHA256 checksum.
func VerifyChecksum(expected string, data []byte) bool {
	actual := fmt.Sprintf("%x", sha256.Sum256(data))
	return actual == expected
}
