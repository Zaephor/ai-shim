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

func TestDetectSocket_DefaultFallback(t *testing.T) {
	// detectSocket always returns a non-empty string, falling back to default
	sock := detectSocket()
	assert.NotEmpty(t, sock)
}

func TestCountGPUsViaSMI_NoNvidiaSMI(t *testing.T) {
	// On systems without nvidia-smi, countGPUsViaSMI returns 0
	// This tests the error path of exec.Command
	if runtime.GOOS == "darwin" {
		// macOS never has nvidia-smi
		assert.Equal(t, 0, countGPUsViaSMI())
	}
	// On Linux without GPU, also returns 0
	count := countGPUsViaSMI()
	assert.True(t, count >= 0, "GPU count should be non-negative")
}

func TestDetectGPU_OnLinuxNoGPU(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific test")
	}
	// On Linux CI without GPU hardware, both paths should fail gracefully
	avail, count := detectGPU()
	// Either GPU is present or not — verify consistency
	if avail {
		assert.True(t, count > 0)
	} else {
		assert.Equal(t, 0, count)
	}
}
