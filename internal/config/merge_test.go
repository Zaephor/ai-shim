package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func boolPtr(b bool) *bool { return &b }

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
	base := Config{DIND: boolPtr(true), GPU: boolPtr(false)}
	over := Config{DIND: boolPtr(false)}
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
	base := Config{DINDCache: boolPtr(false)}
	over := Config{DINDCache: boolPtr(true)}
	result := Merge(base, over)
	assert.True(t, *result.DINDCache)
}
