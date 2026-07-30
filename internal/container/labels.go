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
	// LabelSession uniquely identifies one session. Its value is the agent
	// container name, which Docker guarantees is unique daemon-wide. Every
	// container, volume and network created for a session inherits it via
	// spec.Labels, and sidecar teardown matches on it so ending one session
	// cannot touch a parallel sibling's sidecars.
	//
	// On shared network scopes (global/profile/workspace) EnsureNetwork
	// applies labels only when it creates the network, so the label there
	// names the session that created it. Harmless: network cleanup is
	// attachment-count based, never label-identity based.
	LabelSession = "ai-shim.session"
)
