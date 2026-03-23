package platform

import (
	"os"
	"os/user"
	"runtime"
	"strconv"
)

type Info struct {
	DockerSocket string
	UID          int
	GID          int
	Username     string
	Hostname     string
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
}
