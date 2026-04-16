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
	if over.SymlinkDir != "" {
		result.SymlinkDir = over.SymlinkDir
	}
	if over.SelfUpdate != nil {
		if result.SelfUpdate == nil {
			result.SelfUpdate = &SelfUpdateConfig{}
		}
		if over.SelfUpdate.Repository != "" {
			result.SelfUpdate.Repository = over.SelfUpdate.Repository
		}
		if over.SelfUpdate.APIURL != "" {
			result.SelfUpdate.APIURL = over.SelfUpdate.APIURL
		}
		if over.SelfUpdate.Enabled != nil {
			result.SelfUpdate.Enabled = over.SelfUpdate.Enabled
		}
		if over.SelfUpdate.Prerelease != nil {
			result.SelfUpdate.Prerelease = over.SelfUpdate.Prerelease
		}
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
	result.ToolsOrder = mergeOrder(result.ToolsOrder, over.ToolsOrder)
	result.MCPServers = mergeMCPServerMaps(result.MCPServers, over.MCPServers)
	result.MCPServersOrder = mergeOrder(result.MCPServersOrder, over.MCPServersOrder)

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

// mergeOrder concatenates two ordered key lists while preserving the first
// occurrence of each key. Used for ToolsOrder so that lower-precedence
// tiers (default → agent → profile → agent-profile) contribute the install
// order of their distinct tools and overrides don't reshuffle the list.
func mergeOrder(base, over []string) []string {
	if len(over) == 0 {
		return base
	}
	if len(base) == 0 {
		out := make([]string, len(over))
		copy(out, over)
		return out
	}
	seen := make(map[string]struct{}, len(base)+len(over))
	out := make([]string, 0, len(base)+len(over))
	for _, k := range base {
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	for _, k := range over {
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
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
		existing, ok := result[k]
		if !ok {
			result[k] = v
			continue
		}
		result[k] = mergeToolDef(existing, v)
	}
	return result
}

// mergeToolDef merges two ToolDef values field-by-field. For each scalar
// field, `over`'s value wins only when it is non-zero / set; otherwise the
// base value is preserved. Files uses appendUnique to combine both sides.
func mergeToolDef(base, over ToolDef) ToolDef {
	result := base
	if over.Type != "" {
		result.Type = over.Type
	}
	if over.URL != "" {
		result.URL = over.URL
	}
	if over.Binary != "" {
		result.Binary = over.Binary
	}
	result.Files = appendUnique(result.Files, over.Files)
	if over.Package != "" {
		result.Package = over.Package
	}
	if over.Install != "" {
		result.Install = over.Install
	}
	if over.Checksum != "" {
		result.Checksum = over.Checksum
	}
	if over.DataDir {
		result.DataDir = over.DataDir
	}
	if over.CacheScope != "" {
		result.CacheScope = over.CacheScope
	}
	if over.EnvVar != "" {
		result.EnvVar = over.EnvVar
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
