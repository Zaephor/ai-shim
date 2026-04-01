package install

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update-golden", false, "update golden files")

// goldenCases defines the entrypoint parameter sets for golden testing.
// Each case captures a distinct combination of install type, version pinning,
// and update interval. If any of these change, the golden file will diff.
var goldenCases = map[string]EntrypointParams{
	"npm_default": {
		InstallType:    "npm",
		Package:        "opencode-ai",
		Binary:         "opencode",
		AgentName:      "opencode",
		UpdateInterval: 86400,
	},
	"npm_version_pinned": {
		InstallType:    "npm",
		Package:        "opencode-ai",
		Binary:         "opencode",
		Version:        "1.3.0",
		AgentName:      "opencode",
		UpdateInterval: 86400,
	},
	"npm_always_update": {
		InstallType:    "npm",
		Package:        "opencode-ai",
		Binary:         "opencode",
		AgentName:      "opencode",
		UpdateInterval: 0,
	},
	"npm_never_update": {
		InstallType:    "npm",
		Package:        "opencode-ai",
		Binary:         "opencode",
		AgentName:      "opencode",
		UpdateInterval: -1,
	},
	"uv_default": {
		InstallType:    "uv",
		Package:        "aider-chat",
		Binary:         "aider",
		AgentName:      "aider",
		UpdateInterval: 86400,
	},
	"uv_version_pinned": {
		InstallType:    "uv",
		Package:        "aider-chat",
		Binary:         "aider",
		Version:        "0.50.0",
		AgentName:      "aider",
		UpdateInterval: 86400,
	},
	"custom_default": {
		InstallType:    "custom",
		Package:        "curl -fsSL https://claude.ai/install.sh | bash",
		Binary:         "claude",
		AgentName:      "claude-code",
		UpdateInterval: 86400,
	},
	"npm_with_args": {
		InstallType:    "npm",
		Package:        "opencode-ai",
		Binary:         "opencode",
		AgentName:      "opencode",
		AgentArgs:      []string{"--verbose", "--no-telemetry"},
		UpdateInterval: 86400,
	},
}

// TestEntrypoint_Golden compares generated entrypoint scripts against golden
// files stored in testdata/. If the output changes, the test fails with a diff
// showing exactly what changed. Run with -update-golden to regenerate:
//
//	go test ./internal/install/ -run Golden -update-golden
func TestEntrypoint_Golden(t *testing.T) {
	for name, params := range goldenCases {
		t.Run(name, func(t *testing.T) {
			got := GenerateEntrypoint(params)
			goldenPath := filepath.Join("testdata", name+".golden.sh")

			if *updateGolden {
				require.NoError(t, os.MkdirAll("testdata", 0755))
				require.NoError(t, os.WriteFile(goldenPath, []byte(got), 0644))
				t.Logf("updated %s", goldenPath)
				return
			}

			want, err := os.ReadFile(goldenPath)
			if os.IsNotExist(err) {
				t.Fatalf("golden file %s not found — run with -update-golden to create it", goldenPath)
			}
			require.NoError(t, err)

			assert.Equal(t, string(want), got,
				"entrypoint output changed — if intentional, run: go test ./internal/install/ -run Golden -update-golden")
		})
	}
}
