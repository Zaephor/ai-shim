package platform

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetect_HasHostname(t *testing.T) {
	info := Detect()
	assert.NotEmpty(t, info.Hostname)
}

func TestDetect_HasUsername(t *testing.T) {
	info := Detect()
	assert.NotEmpty(t, info.Username)
}

func TestDetect_HasUID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UID not applicable on Windows")
	}
	info := Detect()
	assert.True(t, info.UID >= 0)
}

func TestDetect_SocketPath(t *testing.T) {
	info := Detect()
	assert.NotEmpty(t, info.DockerSocket)
}
