package cli

import (
	"encoding/json"
	"os"
)

// IsJSONMode returns true if AI_SHIM_JSON=1 is set.
func IsJSONMode() bool {
	return os.Getenv("AI_SHIM_JSON") == "1"
}

// MarshalJSON marshals v to a JSON string with indentation.
func MarshalJSON(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

// StatusEntry represents a container in JSON status output.
type StatusEntry struct {
	Name    string `json:"name"`
	Agent   string `json:"agent"`
	Profile string `json:"profile"`
	Image   string `json:"image"`
	Status  string `json:"status"`
}

// AgentEntry represents an agent definition in JSON output.
type AgentEntry struct {
	Name        string `json:"name"`
	InstallType string `json:"install_type"`
	Binary      string `json:"binary"`
}

// DoctorResult represents the result of a doctor check in JSON output.
type DoctorResult struct {
	Docker       DoctorCheck   `json:"docker"`
	DefaultImage DoctorCheck   `json:"default_image"`
	StorageRoot  string        `json:"storage_root"`
	ConfigDir    string        `json:"config_dir"`
	ImagePinning []PinStatus   `json:"image_pinning"`
}

// DoctorCheck represents a single diagnostic check.
type DoctorCheck struct {
	Status  string `json:"status"` // "ok", "fail", "not_cached"
	Detail  string `json:"detail,omitempty"`
}

// PinStatus represents the pinning status of an image.
type PinStatus struct {
	Label  string `json:"label"` // "agent", "dind", "cache"
	Image  string `json:"image"`
	Pinned bool   `json:"pinned"`
}

// DiskUsageResult represents disk usage in JSON output.
type DiskUsageResult struct {
	Directories []DiskUsageEntry `json:"directories"`
	Total       int64            `json:"total_bytes"`
	Profiles    []DiskUsageEntry `json:"profiles,omitempty"`
}

// DiskUsageEntry represents a single directory's disk usage.
type DiskUsageEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path,omitempty"`
	Bytes int64  `json:"bytes"`
}
