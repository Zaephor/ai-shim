package config

import (
	"testing"

	"github.com/Zaephor/ai-shim/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMerge_ScalarsLastWins(t *testing.T) {
	base := Config{Image: "base-image", Hostname: "base-host", Version: "1.0"}
	over := Config{Image: "over-image", Version: "2.0"}
	result := Merge(base, over)
	assert.Equal(t, "over-image", result.Image)
	assert.Equal(t, "base-host", result.Hostname, "unset scalar should not overwrite")
	assert.Equal(t, "2.0", result.Version)
}

func TestMerge_MapsPerKeyReplace(t *testing.T) {
	base := Config{
		Env:       map[string]string{"A": "1", "B": "2"},
		Variables: map[string]string{"X": "10"},
	}
	over := Config{
		Env:       map[string]string{"B": "override", "C": "3"},
		Variables: map[string]string{"Y": "20"},
	}
	result := Merge(base, over)
	assert.Equal(t, "1", result.Env["A"], "untouched key preserved")
	assert.Equal(t, "override", result.Env["B"], "overlapping key replaced")
	assert.Equal(t, "3", result.Env["C"], "new key added")
	assert.Equal(t, "10", result.Variables["X"])
	assert.Equal(t, "20", result.Variables["Y"])
}

func TestMerge_ListsAppend(t *testing.T) {
	base := Config{
		Volumes: []string{"/a:/a"},
		Args:    []string{"--flag1"},
		Ports:   []string{"8080:8080"},
	}
	over := Config{
		Volumes: []string{"/b:/b"},
		Args:    []string{"--flag2"},
		Ports:   []string{"9090:9090"},
	}
	result := Merge(base, over)
	assert.Equal(t, []string{"/a:/a", "/b:/b"}, result.Volumes)
	assert.Equal(t, []string{"--flag1", "--flag2"}, result.Args)
	assert.Equal(t, []string{"8080:8080", "9090:9090"}, result.Ports)
}

func TestMerge_BoolPtrsLastWins(t *testing.T) {
	base := Config{DIND: testutil.BoolPtr(true), GPU: testutil.BoolPtr(false)}
	over := Config{DIND: testutil.BoolPtr(false)}
	result := Merge(base, over)
	assert.Equal(t, false, *result.DIND, "overridden bool")
	assert.Equal(t, false, *result.GPU, "preserved bool")
}

func TestMerge_ToolsPerKeyReplace(t *testing.T) {
	base := Config{
		Tools: map[string]ToolDef{
			"act":  {Type: "tar-extract", URL: "old-url"},
			"helm": {Type: "binary-download", URL: "helm-url"},
		},
	}
	over := Config{
		Tools: map[string]ToolDef{
			"act": {Type: "tar-extract", URL: "new-url"},
		},
	}
	result := Merge(base, over)
	assert.Equal(t, "new-url", result.Tools["act"].URL, "tool replaced entirely")
	assert.Equal(t, "helm-url", result.Tools["helm"].URL, "untouched tool preserved")
}

func TestMerge_ToolsFieldByField(t *testing.T) {
	base := Config{
		Tools: map[string]ToolDef{
			"nvm": {
				Type:    "custom",
				URL:     "base-url",
				DataDir: true,
				EnvVar:  "NVM_DIR",
				Install: "base-install",
			},
		},
	}
	over := Config{
		Tools: map[string]ToolDef{
			"nvm": {URL: "override-url"},
		},
	}
	result := Merge(base, over)
	nvm := result.Tools["nvm"]
	assert.Equal(t, "override-url", nvm.URL, "URL should be overridden")
	assert.True(t, nvm.DataDir, "DataDir from base should be preserved")
	assert.Equal(t, "NVM_DIR", nvm.EnvVar, "EnvVar from base should be preserved")
	assert.Equal(t, "base-install", nvm.Install, "Install from base should be preserved")
	assert.Equal(t, "custom", nvm.Type, "Type from base should be preserved")
}

// TestMerge_ToolsOrderAndFieldByField covers the interaction between
// ToolsOrder first-occurrence preservation and per-tool field-by-field
// merging. A profile tier that both partially overrides an existing tool
// and introduces a new tool should (a) preserve the base order with the
// new tool appended, and (b) keep the base tool's untouched fields.
func TestMerge_ToolsOrderAndFieldByField(t *testing.T) {
	base := Config{
		Tools: map[string]ToolDef{
			"nvm": {
				Type:    "custom",
				URL:     "base-nvm-url",
				DataDir: true,
				EnvVar:  "NVM_DIR",
				Install: "base-nvm-install",
			},
			"ruff": {
				Type:    "binary",
				URL:     "base-ruff-url",
				Binary:  "ruff",
				Install: "base-ruff-install",
			},
		},
		ToolsOrder: []string{"nvm", "ruff"},
	}
	over := Config{
		Tools: map[string]ToolDef{
			"nvm": {URL: "override-nvm-url"},
			"pre-commit": {
				Type:    "pip",
				Package: "pre-commit",
				Install: "pre-commit-install",
			},
		},
		ToolsOrder: []string{"nvm", "pre-commit"},
	}
	result := Merge(base, over)

	assert.Equal(t, []string{"nvm", "ruff", "pre-commit"}, result.ToolsOrder,
		"base order preserved; new entries appended in over's declaration order")

	nvm := result.Tools["nvm"]
	assert.Equal(t, "override-nvm-url", nvm.URL, "URL should be overridden")
	assert.Equal(t, "custom", nvm.Type, "Type from base preserved on partial override")
	assert.True(t, nvm.DataDir, "DataDir from base preserved on partial override")
	assert.Equal(t, "NVM_DIR", nvm.EnvVar, "EnvVar from base preserved on partial override")
	assert.Equal(t, "base-nvm-install", nvm.Install, "Install from base preserved on partial override")

	ruff := result.Tools["ruff"]
	assert.Equal(t, "base-ruff-url", ruff.URL, "untouched tool unchanged")
	assert.Equal(t, "ruff", ruff.Binary, "untouched tool unchanged")
	assert.Equal(t, "base-ruff-install", ruff.Install, "untouched tool unchanged")

	preCommit := result.Tools["pre-commit"]
	assert.Equal(t, "pip", preCommit.Type, "new tool inserted with full def")
	assert.Equal(t, "pre-commit", preCommit.Package, "new tool inserted with full def")
	assert.Equal(t, "pre-commit-install", preCommit.Install, "new tool inserted with full def")
}

// TestMerge_MCPServersOrderAndFieldByField mirrors the tools interaction
// test for MCP servers. mergeMCPServerMaps is currently total-replace per
// key (no field-by-field merge), so this test asserts the current
// behavior: a partial override on an existing server replaces the entire
// definition, while a new server is appended to MCPServersOrder.
//
// TODO: mirror tool per-field merge if we ever need partial MCP overrides.
func TestMerge_MCPServersOrderAndFieldByField(t *testing.T) {
	base := Config{
		MCPServers: map[string]MCPServerDef{
			"filesystem": {
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem"},
				Env:     map[string]string{"FS_ROOT": "/workspace"},
			},
			"git": {
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-git"},
			},
		},
		MCPServersOrder: []string{"filesystem", "git"},
	}
	over := Config{
		MCPServers: map[string]MCPServerDef{
			"filesystem": {Command: "override-cmd"},
			"postgres": {
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-postgres"},
			},
		},
		MCPServersOrder: []string{"filesystem", "postgres"},
	}
	result := Merge(base, over)

	assert.Equal(t, []string{"filesystem", "git", "postgres"}, result.MCPServersOrder,
		"base order preserved; new entries appended in over's declaration order")

	// Current behavior: total-replace per key. Base fields (Args, Env) are
	// NOT preserved when over supplies a partial definition.
	fs := result.MCPServers["filesystem"]
	assert.Equal(t, "override-cmd", fs.Command, "Command replaced by over")
	assert.Nil(t, fs.Args, "total-replace: base Args not preserved (current behavior)")
	assert.Nil(t, fs.Env, "total-replace: base Env not preserved (current behavior)")

	// Untouched base entry is preserved as-is.
	gitSrv := result.MCPServers["git"]
	assert.Equal(t, "npx", gitSrv.Command)
	assert.Equal(t, []string{"-y", "@modelcontextprotocol/server-git"}, gitSrv.Args)

	// New entry is inserted with its full definition.
	pg := result.MCPServers["postgres"]
	assert.Equal(t, "npx", pg.Command)
	assert.Equal(t, []string{"-y", "@modelcontextprotocol/server-postgres"}, pg.Args)
}

// TestMerge_MCPServersOrderPreserved verifies that MCPServersOrder from both
// base and over configs are concatenated with first-occurrence wins, mirroring
// how ToolsOrder is merged so overrides don't reshuffle declaration order.
func TestMerge_MCPServersOrderPreserved(t *testing.T) {
	base := Config{
		MCPServers: map[string]MCPServerDef{
			"filesystem": {Command: "npx"},
			"git":        {Command: "npx"},
		},
		MCPServersOrder: []string{"filesystem", "git"},
	}
	over := Config{
		MCPServers: map[string]MCPServerDef{
			"git":      {Command: "npx-override"},
			"postgres": {Command: "npx"},
		},
		MCPServersOrder: []string{"git", "postgres"},
	}
	result := Merge(base, over)
	assert.Equal(t, []string{"filesystem", "git", "postgres"}, result.MCPServersOrder,
		"first-occurrence order preserved; duplicates not reshuffled")
}

func TestMergeAll_FiveTiers(t *testing.T) {
	tiers := []Config{
		{Image: "default-image", Env: map[string]string{"A": "1"}, Volumes: []string{"/default"}},
		{Env: map[string]string{"A": "agent-override"}},
		{Volumes: []string{"/profile"}},
		{Image: "agent-profile-image"},
		{Env: map[string]string{"A": "env-override"}},
	}
	result := MergeAll(tiers...)
	assert.Equal(t, "agent-profile-image", result.Image)
	assert.Equal(t, "env-override", result.Env["A"])
	assert.Equal(t, []string{"/default", "/profile"}, result.Volumes)
}

func TestMerge_NilMaps(t *testing.T) {
	base := Config{Env: nil}
	over := Config{Env: map[string]string{"A": "1"}}
	result := Merge(base, over)
	assert.Equal(t, "1", result.Env["A"])
}

func TestMerge_BothNilMaps(t *testing.T) {
	base := Config{Env: nil}
	over := Config{Env: nil}
	result := Merge(base, over)
	assert.Nil(t, result.Env)
}

func TestMerge_DINDHostname(t *testing.T) {
	base := Config{DINDHostname: "default-dind"}
	over := Config{DINDHostname: "custom-dind"}
	result := Merge(base, over)
	assert.Equal(t, "custom-dind", result.DINDHostname)
}

func TestMerge_EmptyListAppend(t *testing.T) {
	base := Config{Args: nil}
	over := Config{Args: []string{"--flag"}}
	result := Merge(base, over)
	assert.Equal(t, []string{"--flag"}, result.Args)
}

func TestMerge_DINDMirrors(t *testing.T) {
	base := Config{DINDMirrors: []string{"https://mirror1.example.com"}}
	over := Config{DINDMirrors: []string{"https://mirror2.example.com"}}
	result := Merge(base, over)
	assert.Contains(t, result.DINDMirrors, "https://mirror1.example.com")
	assert.Contains(t, result.DINDMirrors, "https://mirror2.example.com")
}

func TestMerge_DINDCache(t *testing.T) {
	base := Config{DINDCache: testutil.BoolPtr(false)}
	over := Config{DINDCache: testutil.BoolPtr(true)}
	result := Merge(base, over)
	assert.True(t, *result.DINDCache)
}

func TestMerge_Resources(t *testing.T) {
	base := Config{Resources: &ResourceLimits{Memory: "2g", CPUs: "1.0"}}
	over := Config{Resources: &ResourceLimits{Memory: "4g", CPUs: "2.0"}}
	result := Merge(base, over)
	assert.Equal(t, "4g", result.Resources.Memory)
	assert.Equal(t, "2.0", result.Resources.CPUs)
}

func TestMerge_ResourcesNilPreserved(t *testing.T) {
	base := Config{Resources: &ResourceLimits{Memory: "2g"}}
	over := Config{} // nil Resources
	result := Merge(base, over)
	assert.Equal(t, "2g", result.Resources.Memory, "nil override should not clear resources")
}

func TestMerge_DINDResources(t *testing.T) {
	base := Config{}
	over := Config{DINDResources: &ResourceLimits{Memory: "1g"}}
	result := Merge(base, over)
	require.NotNil(t, result.DINDResources)
	assert.Equal(t, "1g", result.DINDResources.Memory)
}

func TestMerge_DINDTLS(t *testing.T) {
	base := Config{DINDTLS: testutil.BoolPtr(false)}
	over := Config{DINDTLS: testutil.BoolPtr(true)}
	result := Merge(base, over)
	assert.True(t, *result.DINDTLS)
}

func TestMerge_DINDTLSNilPreserved(t *testing.T) {
	base := Config{DINDTLS: testutil.BoolPtr(true)}
	over := Config{}
	result := Merge(base, over)
	require.NotNil(t, result.DINDTLS)
	assert.True(t, *result.DINDTLS)
}

func TestMerge_GitConfig(t *testing.T) {
	base := Config{Git: &GitConfig{Name: "Base User", Email: "base@example.com"}}
	over := Config{Git: &GitConfig{Name: "Override User"}}
	result := Merge(base, over)
	require.NotNil(t, result.Git)
	assert.Equal(t, "Override User", result.Git.Name, "name should be overridden")
	assert.Equal(t, "base@example.com", result.Git.Email, "email should be preserved from base")
}

func TestMerge_GitConfigNilPreserved(t *testing.T) {
	base := Config{Git: &GitConfig{Name: "User"}}
	over := Config{}
	result := Merge(base, over)
	require.NotNil(t, result.Git)
	assert.Equal(t, "User", result.Git.Name)
}

func TestMerge_GitConfigFromNil(t *testing.T) {
	base := Config{}
	over := Config{Git: &GitConfig{Email: "test@example.com"}}
	result := Merge(base, over)
	require.NotNil(t, result.Git)
	assert.Equal(t, "test@example.com", result.Git.Email)
}

func TestMerge_ResourcesFieldByField(t *testing.T) {
	base := Config{Resources: &ResourceLimits{Memory: "4g"}}
	over := Config{Resources: &ResourceLimits{CPUs: "2.0"}}
	result := Merge(base, over)
	require.NotNil(t, result.Resources)
	assert.Equal(t, "4g", result.Resources.Memory, "memory from base preserved")
	assert.Equal(t, "2.0", result.Resources.CPUs, "cpus from over applied")
}

func TestMerge_GitFieldByField(t *testing.T) {
	base := Config{Git: &GitConfig{Name: "Alice"}}
	over := Config{Git: &GitConfig{Email: "alice@example.com"}}
	result := Merge(base, over)
	require.NotNil(t, result.Git)
	assert.Equal(t, "Alice", result.Git.Name, "name from base preserved")
	assert.Equal(t, "alice@example.com", result.Git.Email, "email from over applied")
}

func TestMerge_ResourcesOverrideWins(t *testing.T) {
	base := Config{Resources: &ResourceLimits{Memory: "2g", CPUs: "1.0"}}
	over := Config{Resources: &ResourceLimits{Memory: "8g", CPUs: "4.0"}}
	result := Merge(base, over)
	require.NotNil(t, result.Resources)
	assert.Equal(t, "8g", result.Resources.Memory, "higher tier memory wins")
	assert.Equal(t, "4.0", result.Resources.CPUs, "higher tier cpus wins")
}

func TestMerge_MCPServersPerKeyReplace(t *testing.T) {
	base := Config{
		MCPServers: map[string]MCPServerDef{
			"filesystem": {Command: "npx", Args: []string{"-y", "fs-server"}},
			"git":        {Command: "npx", Args: []string{"-y", "git-server"}},
		},
	}
	over := Config{
		MCPServers: map[string]MCPServerDef{
			"filesystem": {Command: "node", Args: []string{"fs-server.js"}},
		},
	}
	result := Merge(base, over)
	assert.Equal(t, "node", result.MCPServers["filesystem"].Command, "overridden server replaced")
	assert.Equal(t, "npx", result.MCPServers["git"].Command, "untouched server preserved")
}

func TestMerge_MCPServersNilPreserved(t *testing.T) {
	base := Config{MCPServers: map[string]MCPServerDef{"fs": {Command: "npx"}}}
	over := Config{}
	result := Merge(base, over)
	assert.Equal(t, "npx", result.MCPServers["fs"].Command)
}

func TestMerge_ToolsFromEmpty(t *testing.T) {
	base := Config{}
	over := Config{Tools: map[string]ToolDef{"act": {Type: "binary-download", URL: "url"}}}
	result := Merge(base, over)
	assert.Equal(t, "url", result.Tools["act"].URL, "tools should be added when base is nil")
}

func TestMerge_ToolsOverEmpty(t *testing.T) {
	base := Config{Tools: map[string]ToolDef{"act": {Type: "binary-download", URL: "url"}}}
	over := Config{}
	result := Merge(base, over)
	assert.Equal(t, "url", result.Tools["act"].URL, "tools should be preserved when over is nil")
}

func TestMerge_MCPServersFromEmpty(t *testing.T) {
	base := Config{}
	over := Config{MCPServers: map[string]MCPServerDef{"fs": {Command: "npx"}}}
	result := Merge(base, over)
	assert.Equal(t, "npx", result.MCPServers["fs"].Command, "MCP servers should be added when base is nil")
}

func TestMergeAll_ThreeTierPartialResources(t *testing.T) {
	tier1 := Config{Resources: &ResourceLimits{Memory: "2g"}}
	tier2 := Config{Resources: &ResourceLimits{CPUs: "1.0"}}
	tier3 := Config{Resources: &ResourceLimits{Memory: "8g"}}
	result := MergeAll(tier1, tier2, tier3)
	require.NotNil(t, result.Resources)
	assert.Equal(t, "8g", result.Resources.Memory, "tier3 memory overrides tier1")
	assert.Equal(t, "1.0", result.Resources.CPUs, "tier2 CPUs preserved through tier3")
}

func TestMergeAll_ThreeTierPartialGit(t *testing.T) {
	tier1 := Config{Git: &GitConfig{Name: "Default User"}}
	tier2 := Config{Git: &GitConfig{Email: "agent@example.com"}}
	tier3 := Config{Git: &GitConfig{Name: "Profile User"}}
	result := MergeAll(tier1, tier2, tier3)
	require.NotNil(t, result.Git)
	assert.Equal(t, "Profile User", result.Git.Name, "tier3 name overrides tier1")
	assert.Equal(t, "agent@example.com", result.Git.Email, "tier2 email preserved through tier3")
}

func TestMergeAll_ThreeTierPartialDINDResources(t *testing.T) {
	tier1 := Config{DINDResources: &ResourceLimits{Memory: "1g", CPUs: "0.5"}}
	tier2 := Config{} // no DIND resources
	tier3 := Config{DINDResources: &ResourceLimits{CPUs: "2.0"}}
	result := MergeAll(tier1, tier2, tier3)
	require.NotNil(t, result.DINDResources)
	assert.Equal(t, "1g", result.DINDResources.Memory, "tier1 memory preserved (tier2 nil, tier3 partial)")
	assert.Equal(t, "2.0", result.DINDResources.CPUs, "tier3 CPUs overrides tier1")
}

func TestMergeAll_FiveTierScalarChain(t *testing.T) {
	// Simulate the actual 5-tier config: default → agent → profile → agent-profile → env
	result := MergeAll(
		Config{Image: "default:latest", SecurityProfile: "default", UpdateInterval: "1d"},
		Config{UpdateInterval: "7d"},      // agent overrides interval
		Config{SecurityProfile: "strict"}, // profile overrides security
		Config{},                          // agent-profile: no overrides
		Config{Image: "env:override"},     // env overrides image
	)
	assert.Equal(t, "env:override", result.Image, "env tier wins for image")
	assert.Equal(t, "strict", result.SecurityProfile, "profile tier wins for security")
	assert.Equal(t, "7d", result.UpdateInterval, "agent tier wins for interval (no later override)")
}

func TestMergeAll_Empty(t *testing.T) {
	result := MergeAll()
	assert.Equal(t, Config{}, result, "MergeAll with no configs returns zero config")
}

func TestMerge_UpdateInterval(t *testing.T) {
	base := Config{UpdateInterval: "1d"}
	over := Config{UpdateInterval: "7d"}
	result := Merge(base, over)
	assert.Equal(t, "7d", result.UpdateInterval)
}

func TestMerge_UpdateIntervalPreserved(t *testing.T) {
	base := Config{UpdateInterval: "1d"}
	over := Config{}
	result := Merge(base, over)
	assert.Equal(t, "1d", result.UpdateInterval, "base UpdateInterval should be preserved when over is empty")
}

func TestMerge_SecurityProfile(t *testing.T) {
	base := Config{SecurityProfile: "default"}
	over := Config{SecurityProfile: "strict"}
	result := Merge(base, over)
	assert.Equal(t, "strict", result.SecurityProfile)
}

func TestMerge_SecurityProfileNilPreserved(t *testing.T) {
	base := Config{SecurityProfile: "strict"}
	over := Config{}
	result := Merge(base, over)
	assert.Equal(t, "strict", result.SecurityProfile)
}
