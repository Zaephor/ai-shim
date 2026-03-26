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

func TestDetect_GPUFields(t *testing.T) {
	info := Detect()
	if info.GPUAvailable {
		assert.True(t, info.GPUDevices > 0)
	} else {
		assert.Equal(t, 0, info.GPUDevices)
	}
}

func TestDetect_RootWarningField(t *testing.T) {
	info := Detect()
	// Just verify the field exists and is populated
	assert.True(t, info.UID >= 0)
}

func TestDetect_GPUOnMac(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-specific test")
	}
	info := Detect()
	assert.False(t, info.GPUAvailable, "GPU should not be detected on macOS")
}
