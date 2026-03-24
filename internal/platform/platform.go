package platform

import (
	"os"
<<<<<<< HEAD
	"os/user"
	"runtime"
	"strconv"
)

=======
	"os/exec"
	"os/user"
	"runtime"
	"strings"
)

// Info holds detected platform information.
>>>>>>> 125fbd4 (feat(platform): add NVIDIA GPU detection for Linux)
type Info struct {
	DockerSocket string
	UID          int
	GID          int
	Username     string
	Hostname     string
<<<<<<< HEAD
}

func Detect() Info {
	info := Info{
		DockerSocket: detectSocket(),
		Hostname:     detectHostname(),
	}
	if u, err := user.Current(); err == nil {
		info.Username = u.Username
		info.UID, _ = strconv.Atoi(u.Uid)
		info.GID, _ = strconv.Atoi(u.Gid)
	}
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

func detectHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "localhost"
	}
	return h
=======
	GPUAvailable bool // true if NVIDIA GPU detected
	GPUDevices   int  // number of GPU devices found
}

// Detect gathers platform information for the current host.
func Detect() Info {
	info := Info{}

	// Docker socket
	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		info.DockerSocket = "/var/run/docker.sock"
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

// detectGPU checks for NVIDIA GPU availability.
func detectGPU() (available bool, count int) {
	if runtime.GOOS == "darwin" {
		// Apple MPS is a different paradigm; report no NVIDIA GPU.
		return false, 0
	}

	// Linux: check for /dev/nvidia0 first
	if _, err := os.Stat("/dev/nvidia0"); err == nil {
		// Device node exists; try nvidia-smi for an accurate count.
		if n := countGPUsViaSMI(); n > 0 {
			return true, n
		}
		// Fallback: at least one GPU is present.
		return true, 1
	}

	// Try nvidia-smi even without device node (driver may expose GPUs differently).
	if n := countGPUsViaSMI(); n > 0 {
		return true, n
	}

	return false, 0
}

// countGPUsViaSMI runs nvidia-smi --list-gpus and counts output lines.
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
>>>>>>> 125fbd4 (feat(platform): add NVIDIA GPU detection for Linux)
}
