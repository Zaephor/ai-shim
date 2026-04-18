package platform

import (
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
)

// Info holds detected platform information.
type Info struct {
	DockerSocket string
	UID          int
	GID          int
	Username     string
	Hostname     string
	GPUAvailable bool // true if NVIDIA GPU detected
	GPUDevices   int  // number of GPU devices found
}

// Detect gathers platform information for the current host.
func Detect() Info {
	info := Info{
		DockerSocket: detectSocket(),
	}

	// User info
	if u, err := user.Current(); err == nil {
		info.Username = u.Username
	}

	// Hostname
	if h, err := os.Hostname(); err == nil {
		info.Hostname = h
	}

	// UID/GID (set in platform-specific files)
	info.UID, info.GID = getIDs()

	// GPU detection
	info.GPUAvailable, info.GPUDevices = detectGPU()

	return info
}

func detectSocket() string {
	candidates := []string{"/var/run/docker.sock"}
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		if home != "" {
			candidates = append(candidates,
				home+"/.docker/run/docker.sock",
				home+"/.colima/default/docker.sock",
				home+"/.colima/docker.sock",
			)
		}
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "/var/run/docker.sock"
}

// detectGPU checks for NVIDIA GPU availability.
func detectGPU() (available bool, count int) {
	if runtime.GOOS == "darwin" {
		return false, 0
	}
	n := countGPUsViaSMI()
	if n > 0 {
		return true, n
	}
	if _, err := os.Stat("/dev/nvidia0"); err == nil {
		return true, 1
	}
	return false, 0
}

func countGPUsViaSMI() int {
	out, err := exec.Command("nvidia-smi", "--list-gpus").Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	count := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			count++
		}
	}
	return count
}
