package provision

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"

	"github.com/ai-shim/ai-shim/internal/shell"
)

var hexPattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// isValidChecksum validates that a checksum string is exactly 64 hex characters (SHA256).
func isValidChecksum(s string) bool {
	return hexPattern.MatchString(s)
}

// ToolDef mirrors config.ToolDef — the tool definition from config.
type ToolDef struct {
	Type     string // binary-download, tar-extract, tar-extract-selective, apt, go-install, custom
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
		fmt.Fprintf(&b, "\n# Install: %s\n", name)
		b.WriteString(generateToolInstall(tool, targetDir))
	}

	return b.String()
}

func generateToolInstall(tool ToolDef, targetDir string) string {
	var b strings.Builder

	bin := shell.Quote(tool.Binary)
	url := shell.Quote(tool.URL)
	pkg := shell.Quote(tool.Package)

	switch tool.Type {
	case "binary-download":
		fmt.Fprintf(&b, "if [ ! -f \"%s\"/%s ]; then\n", targetDir, bin)
		fmt.Fprintf(&b, "  curl -fsSL -o \"%s\"/%s %s\n", targetDir, bin, url)
		fmt.Fprintf(&b, "  chmod +x \"%s\"/%s\n", targetDir, bin)
		if tool.Checksum != "" && isValidChecksum(tool.Checksum) {
			fmt.Fprintf(&b, "  echo '%s  %s/%s' | sha256sum -c - || { echo \"ERROR: checksum verification failed for %s\"; exit 1; }\n", tool.Checksum, targetDir, bin, bin)
		}
		b.WriteString("fi\n")

	case "tar-extract":
		fmt.Fprintf(&b, "if [ ! -f \"%s\"/%s ]; then\n", targetDir, bin)
		fmt.Fprintf(&b, "  curl -fsSL %s | tar xz -C \"%s\" --strip-components=1 --wildcards '*/'%s || \\\n", url, targetDir, bin)
		fmt.Fprintf(&b, "  { echo \"Fallback: extracting %s via find...\"; curl -fsSL %s | tar xz -C /tmp && find /tmp -name %s -exec mv {} \"%s/\" \\; ; } || \\\n", bin, url, bin, targetDir)
		fmt.Fprintf(&b, "  { echo \"ERROR: tar extract failed for %s\"; exit 1; }\n", bin)
		fmt.Fprintf(&b, "  chmod +x \"%s\"/%s\n", targetDir, bin)
		b.WriteString("fi\n")

	case "tar-extract-selective":
		fmt.Fprintf(&b, "if [ ! -f \"%s\"/%s ]; then\n", targetDir, bin)
		files := append([]string{tool.Binary}, tool.Files...)
		wildcards := make([]string, len(files))
		for i, f := range files {
			wildcards[i] = fmt.Sprintf("'*/'%s", shell.Quote(f))
		}
		fmt.Fprintf(&b, "  curl -fsSL %s | tar xz -C \"%s\" --strip-components=1 --wildcards %s || { echo \"ERROR: tar extract failed for %s\"; exit 1; }\n",
			url, targetDir, strings.Join(wildcards, " "), bin)
		fmt.Fprintf(&b, "  chmod +x \"%s\"/%s\n", targetDir, bin)
		b.WriteString("fi\n")

	case "apt":
		fmt.Fprintf(&b, "if ! command -v %s >/dev/null 2>&1; then\n", bin)
		fmt.Fprintf(&b, "  apt-get update -qq && apt-get install -y -qq %s || { echo \"ERROR: apt install failed for %s\"; exit 1; }\n", pkg, pkg)
		b.WriteString("fi\n")

	case "go-install":
		fmt.Fprintf(&b, "if [ ! -f \"%s\"/%s ]; then\n", targetDir, bin)
		fmt.Fprintf(&b, "  GOBIN=\"%s\" go install %s@latest\n", targetDir, pkg)
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
