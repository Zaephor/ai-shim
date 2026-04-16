package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadFile loads a Config from a YAML file. Returns an empty Config if the file
// does not exist (not an error — missing tiers are normal).
func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if len(data) == 0 {
		return cfg, nil
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	cfg.ToolsOrder = extractToolsOrder(data)
	cfg.MCPServersOrder = extractMCPServersOrder(data)
	return cfg, nil
}

// extractToolsOrder parses a raw YAML document and returns the ordered list
// of keys under the top-level `tools` mapping. This captures the declaration
// order so the provisioning script can install tools in the sequence the
// user wrote them rather than in Go map-iteration (random) order.
//
// It is invoked alongside yaml.Unmarshal rather than via a custom
// UnmarshalYAML method on Config so that yaml.Decoder's KnownFields/strict
// mode (used by LoadFileStrict) still sees the full document structure and
// can still surface unknown-key warnings.
func extractToolsOrder(data []byte) []string {
	return extractTopLevelMappingOrder(data, "tools")
}

// extractMCPServersOrder parses a raw YAML document and returns the ordered
// list of keys under the top-level `mcp_servers` mapping. Mirrors
// extractToolsOrder so the MCP_SERVERS JSON blob emitted to agents preserves
// the user's declaration order rather than Go map-iteration (random) order.
func extractMCPServersOrder(data []byte) []string {
	return extractTopLevelMappingOrder(data, "mcp_servers")
}

// extractTopLevelMappingOrder walks a raw YAML document and returns the
// ordered list of keys under the named top-level mapping, or nil if the key
// is absent or the document is not a mapping. Shared by extractToolsOrder
// and extractMCPServersOrder.
func extractTopLevelMappingOrder(data []byte, key string) []string {
	if len(data) == 0 {
		return nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	// Documents wrap their root mapping in a DocumentNode.
	root := &doc
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if k.Value != key || v.Kind != yaml.MappingNode {
			continue
		}
		order := make([]string, 0, len(v.Content)/2)
		for j := 0; j+1 < len(v.Content); j += 2 {
			order = append(order, v.Content[j].Value)
		}
		return order
	}
	return nil
}

// LoadFileStrict loads a Config from a YAML file and returns warnings for any
// unrecognized keys. The config is still loaded even if unknown keys exist —
// warnings are informational, not fatal.
func LoadFileStrict(path string) (Config, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil, nil
		}
		return Config{}, nil, err
	}
	if len(data) == 0 {
		return Config{}, nil, nil
	}

	// Try strict decode first to detect unknown keys
	var warnings []string
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var strict Config
	if err := dec.Decode(&strict); err != nil {
		// EOF means the file has no YAML documents (e.g., only comments).
		// This is valid — treat as empty config, no warnings.
		if errors.Is(err, io.EOF) {
			return Config{}, nil, nil
		}
		// Extract unknown field warnings from the error
		if w := extractUnknownFieldWarnings(path, err); len(w) > 0 {
			warnings = w
		} else {
			// Real parse error, not just unknown fields
			return Config{}, nil, err
		}
	}

	// Lenient parse to get the actual config (ignoring unknown keys)
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, nil, err
	}
	cfg.ToolsOrder = extractToolsOrder(data)
	cfg.MCPServersOrder = extractMCPServersOrder(data)
	return cfg, warnings, nil
}

// extractUnknownFieldWarnings parses yaml.v3 strict decode errors to produce
// user-friendly warnings. Returns nil if the error is not about unknown fields.
func extractUnknownFieldWarnings(path string, err error) []string {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// yaml.v3 KnownFields errors look like: "line N: field <name> not found in type config.Config"
	if !strings.Contains(msg, "not found in type") {
		return nil
	}
	var warnings []string
	for _, line := range strings.Split(msg, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "not found in type") {
			warnings = append(warnings, fmt.Sprintf("%s: %s", path, line))
		}
	}
	return warnings
}
