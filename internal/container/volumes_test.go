package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVolume_Modes(t *testing.T) {
	tests := []struct {
		in   string
		want Volume
	}{
		{"/host/a:/ctr/a", Volume{Source: "/host/a", Target: "/ctr/a", ReadOnly: false}},
		{"/host/a:/ctr/a:rw", Volume{Source: "/host/a", Target: "/ctr/a", ReadOnly: false}},
		{"/host/a:/ctr/a:ro", Volume{Source: "/host/a", Target: "/ctr/a", ReadOnly: true}},
		{"/host/a:/ctr/a/:ro", Volume{Source: "/host/a", Target: "/ctr/a", ReadOnly: true}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseVolume(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseVolume_Rejects(t *testing.T) {
	tests := []struct {
		in      string
		errPart string
	}{
		{"no-colon-here", "expected source:target"},
		{"/host/a:/ctr/a:z", `unknown mode "z"`},
		{"/host/a:/ctr/a:RO", `unknown mode "RO"`},
		{"/host/a:/ctr/a:", `unknown mode ""`},
		{"/host/a:/ctr/a:ro:rw", "expected source:target"},
		{":/ctr/a", "empty"},
		{"/host/a:", "empty"},
		{"/host/a:relative", "must be absolute"},
		{"/host/../etc/shadow:/ctr/a", "sensitive"},
		{"/etc/passwd:/ctr/passwd", "sensitive"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			_, err := ParseVolume(tt.in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errPart)
		})
	}
}

func TestResolveVolumes_LastTargetWins(t *testing.T) {
	vols, errs, overridden := ResolveVolumes([]string{
		"/host/a:/ctr/x",
		"/host/b:/ctr/y",
		"/host/a:/ctr/x/:ro",
	})
	assert.Empty(t, errs)
	assert.Equal(t, []Volume{
		{Source: "/host/b", Target: "/ctr/y"},
		{Source: "/host/a", Target: "/ctr/x", ReadOnly: true},
	}, vols)
	require.Len(t, overridden, 1)
	assert.Contains(t, overridden[0], `"/host/a:/ctr/x"`)
	assert.Contains(t, overridden[0], `"/host/a:/ctr/x/:ro"`)
}

func TestResolveVolumes_InvalidEntriesReportedAndSkipped(t *testing.T) {
	vols, errs, overridden := ResolveVolumes([]string{
		"/host/ok:/ctr/ok:ro",
		"/host/bad:/ctr/bad:z",
	})
	assert.Equal(t, []Volume{{Source: "/host/ok", Target: "/ctr/ok", ReadOnly: true}}, vols)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "/host/bad:/ctr/bad:z")
	assert.Empty(t, overridden)
}

func TestValidateConfigVolumes_RejectsUnknownMode(t *testing.T) {
	errs := ValidateConfigVolumes([]string{"/host/a:/ctr/a:ro", "/host/b:/ctr/b:rx"})
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), `unknown mode "rx"`)
}

func TestBuildSpec_ReadOnlyVolumes(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Volumes = []string{"/host/ro:/ctr/ro:ro", "/host/rw:/ctr/rw:rw", "/host/def:/ctr/def"}
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	readOnly := map[string]bool{}
	for _, m := range spec.Mounts {
		readOnly[m.Target] = m.ReadOnly
	}
	require.Contains(t, readOnly, "/ctr/ro")
	require.Contains(t, readOnly, "/ctr/rw")
	require.Contains(t, readOnly, "/ctr/def")
	assert.True(t, readOnly["/ctr/ro"], ":ro volume must be mounted read-only")
	assert.False(t, readOnly["/ctr/rw"], ":rw volume must be writable")
	assert.False(t, readOnly["/ctr/def"], "volume without a mode must default to writable")
	assert.NotContains(t, readOnly, "/ctr/ro:ro", "mode suffix must not leak into the target path")
}

func TestBuildSpec_DuplicateVolumeTargetLastWins(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Volumes = []string{"/host/a:/ctr/x", "/host/a:/ctr/x:ro"}
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	var hits []bool
	for _, m := range spec.Mounts {
		if m.Target == "/ctr/x" {
			hits = append(hits, m.ReadOnly)
		}
	}
	assert.Equal(t, []bool{true}, hits, "only the later :ro entry should be mounted")
}
