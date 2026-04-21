package container

const (
	LabelBase         = "ai-shim"
	LabelAgent        = "ai-shim.agent"
	LabelProfile      = "ai-shim.profile"
	LabelRole         = "ai-shim.role" // "agent", "dind", or "cache"
	LabelCache        = "ai-shim.cache"
	LabelUsesCache    = "ai-shim.uses-cache"
	LabelWorkspace    = "ai-shim.workspace"     // workspace hash for directory-scoped lookup
	LabelWorkspaceDir = "ai-shim.workspace.dir" // human-readable path (display only)
	LabelPersistent   = "ai-shim.persistent"    // "true" for detach-capable containers
	LabelDIND         = "ai-shim.dind"          // marks DIND sidecar containers (kept for backward compat)
	LabelVersion      = "ai-shim.version"       // ai-shim version that launched the container (informational)
)
