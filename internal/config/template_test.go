package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveTemplates_EnvVars(t *testing.T) {
	cfg := Config{
		Variables: map[string]string{"llm_host": "my-host:8080"},
		Env: map[string]string{
			"LLM_ENDPOINT": "https://{{ .llm_host }}/v1",
			"STATIC":       "no-template",
		},
	}
	resolved, err := ResolveTemplates(cfg)
	require.NoError(t, err)
	assert.Equal(t, "https://my-host:8080/v1", resolved.Env["LLM_ENDPOINT"])
	assert.Equal(t, "no-template", resolved.Env["STATIC"])
}

func TestResolveTemplates_Volumes(t *testing.T) {
	cfg := Config{
		Variables: map[string]string{"storage_shared": "/home/user/.ai-shim/shared"},
		Volumes:   []string{"{{ .storage_shared }}/bin:/usr/local/bin", "/static:/static"},
	}
	resolved, err := ResolveTemplates(cfg)
	require.NoError(t, err)
	assert.Equal(t, "/home/user/.ai-shim/shared/bin:/usr/local/bin", resolved.Volumes[0])
	assert.Equal(t, "/static:/static", resolved.Volumes[1])
}

func TestResolveTemplates_Image(t *testing.T) {
	cfg := Config{
		Variables: map[string]string{"img_tag": "24.04"},
		Image:     "ubuntu:{{ .img_tag }}",
	}
	resolved, err := ResolveTemplates(cfg)
	require.NoError(t, err)
	assert.Equal(t, "ubuntu:24.04", resolved.Image)
}

func TestResolveTemplates_NoVariables(t *testing.T) {
	cfg := Config{Env: map[string]string{"KEY": "value"}}
	resolved, err := ResolveTemplates(cfg)
	require.NoError(t, err)
	assert.Equal(t, "value", resolved.Env["KEY"])
}

func TestResolveTemplates_UndefinedVariable(t *testing.T) {
	cfg := Config{
		Variables: map[string]string{},
		Env:       map[string]string{"X": "{{ .undefined }}"},
	}
	_, err := ResolveTemplates(cfg)
	assert.Error(t, err, "undefined template variable should error")
}

func TestResolveTemplates_MalformedTemplate(t *testing.T) {
	cfg := Config{
		Env: map[string]string{"X": "{{ .unclosed"},
	}
	_, err := ResolveTemplates(cfg)
	assert.Error(t, err, "malformed template syntax should return error")
}
