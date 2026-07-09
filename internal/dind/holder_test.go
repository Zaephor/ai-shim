package dind

import (
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
)

func TestHolderSpec(t *testing.T) {
	cc, hc := holderSpec(HolderConfig{
		Image:      "docker:dind",
		Hostname:   "ai-shim-dind",
		NetworkID:  "netabc",
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
		Labels:     map[string]string{"k": "v"},
	})
	assert.Equal(t, "docker:dind", cc.Image)
	assert.Equal(t, "ai-shim-dind", cc.Hostname)
	assert.Equal(t, []string{"sleep", "infinity"}, []string(cc.Entrypoint))
	assert.Equal(t, "v", cc.Labels["k"])
	assert.Equal(t, container.NetworkMode("netabc"), hc.NetworkMode)
	assert.Equal(t, []string{"host.docker.internal:host-gateway"}, hc.ExtraHosts)
	// The holder is never auto-restarted: a restart yields a new netns.
	assert.Empty(t, hc.RestartPolicy.Name)
}
