package config

// Merge combines two Configs. The `over` config takes precedence.
// Scalars: over wins if non-zero. Maps: per-key replace. Lists: append.
func Merge(base, over Config) Config {
	result := base

	if over.Image != "" {
		result.Image = over.Image
	}
	if over.Hostname != "" {
		result.Hostname = over.Hostname
	}
	if over.Version != "" {
		result.Version = over.Version
	}
	if over.NetworkScope != "" {
		result.NetworkScope = over.NetworkScope
	}
	if over.DINDHostname != "" {
		result.DINDHostname = over.DINDHostname
	}

	if over.DIND != nil {
		result.DIND = over.DIND
	}
	if over.DINDGpu != nil {
		result.DINDGpu = over.DINDGpu
	}
	if over.GPU != nil {
		result.GPU = over.GPU
	}
	if over.Isolated != nil {
		result.Isolated = over.Isolated
	}
	if over.DINDCache != nil {
		result.DINDCache = over.DINDCache
	}
	if over.DINDTLS != nil {
		result.DINDTLS = over.DINDTLS
	}
	if over.Resources != nil {
		if result.Resources == nil {
			result.Resources = &ResourceLimits{}
		}
		if over.Resources.Memory != "" {
			result.Resources.Memory = over.Resources.Memory
		}
		if over.Resources.CPUs != "" {
			result.Resources.CPUs = over.Resources.CPUs
		}
	}
	if over.DINDResources != nil {
		if result.DINDResources == nil {
			result.DINDResources = &ResourceLimits{}
		}
		if over.DINDResources.Memory != "" {
			result.DINDResources.Memory = over.DINDResources.Memory
		}
		if over.DINDResources.CPUs != "" {
			result.DINDResources.CPUs = over.DINDResources.CPUs
		}
	}
	if over.SecurityProfile != "" {
		result.SecurityProfile = over.SecurityProfile
	}
	if over.UpdateInterval != "" {
		result.UpdateInterval = over.UpdateInterval
	}
	if over.Git != nil {
		if result.Git == nil {
			result.Git = &GitConfig{}
		}
		if over.Git.Name != "" {
			result.Git.Name = over.Git.Name
		}
		if over.Git.Email != "" {
			result.Git.Email = over.Git.Email
		}
	}

	result.Env = mergeMaps(result.Env, over.Env)
	result.Variables = mergeMaps(result.Variables, over.Variables)
	result.Tools = mergeToolMaps(result.Tools, over.Tools)
	result.MCPServers = mergeMCPServerMaps(result.MCPServers, over.MCPServers)

	result.Volumes = appendUnique(result.Volumes, over.Volumes)
	result.Args = append(result.Args, over.Args...)
	result.Ports = appendUnique(result.Ports, over.Ports)
	result.Packages = appendUnique(result.Packages, over.Packages)
	result.AllowAgents = appendUnique(result.AllowAgents, over.AllowAgents)
	result.DINDMirrors = appendUnique(result.DINDMirrors, over.DINDMirrors)

	return result
}

// MergeAll merges multiple configs in order (first = lowest priority).
func MergeAll(configs ...Config) Config {
	if len(configs) == 0 {
		return Config{}
	}
	result := configs[0]
	for _, c := range configs[1:] {
		result = Merge(result, c)
	}
	return result
}

func mergeMaps(base, over map[string]string) map[string]string {
	if len(over) == 0 {
		return base
	}
	if len(base) == 0 {
		result := make(map[string]string, len(over))
		for k, v := range over {
			result[k] = v
		}
		return result
	}
	result := make(map[string]string, len(base)+len(over))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range over {
		result[k] = v
	}
	return result
}

func mergeToolMaps(base, over map[string]ToolDef) map[string]ToolDef {
	if len(over) == 0 {
		return base
	}
	if len(base) == 0 {
		result := make(map[string]ToolDef, len(over))
		for k, v := range over {
			result[k] = v
		}
		return result
	}
	result := make(map[string]ToolDef, len(base)+len(over))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range over {
		result[k] = v
	}
	return result
}

func mergeMCPServerMaps(base, over map[string]MCPServerDef) map[string]MCPServerDef {
	if len(over) == 0 {
		return base
	}
	if len(base) == 0 {
		result := make(map[string]MCPServerDef, len(over))
		for k, v := range over {
			result[k] = v
		}
		return result
	}
	result := make(map[string]MCPServerDef, len(base)+len(over))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range over {
		result[k] = v
	}
	return result
}

func appendUnique(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]bool, len(base))
	for _, v := range base {
		seen[v] = true
	}
	result := make([]string, len(base))
	copy(result, base)
	for _, v := range extra {
		if !seen[v] {
			result = append(result, v)
			seen[v] = true
		}
	}
	return result
}
