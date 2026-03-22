package config

import (
	"bytes"
	"fmt"
	"text/template"
)

// ResolveTemplates resolves {{ .var }} templates in all string fields using
// the Variables map as the data source. Variables themselves are not templated.
func ResolveTemplates(cfg Config) (Config, error) {
	vars := cfg.Variables
	if vars == nil {
		vars = make(map[string]string)
	}

	resolve := func(s string) (string, error) {
		if s == "" {
			return s, nil
		}
		tmpl, err := template.New("").Option("missingkey=error").Parse(s)
		if err != nil {
			return "", fmt.Errorf("parsing template %q: %w", s, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, vars); err != nil {
			return "", fmt.Errorf("executing template %q: %w", s, err)
		}
		return buf.String(), nil
	}

	result := cfg

	var err error
	if result.Image, err = resolve(result.Image); err != nil {
		return Config{}, err
	}
	if result.Hostname, err = resolve(result.Hostname); err != nil {
		return Config{}, err
	}

	if result.Env, err = resolveMap(result.Env, resolve); err != nil {
		return Config{}, err
	}

	if result.Volumes, err = resolveSlice(result.Volumes, resolve); err != nil {
		return Config{}, err
	}
	if result.Ports, err = resolveSlice(result.Ports, resolve); err != nil {
		return Config{}, err
	}

	if len(result.Tools) > 0 {
		resolved := make(map[string]ToolDef, len(result.Tools))
		for k, td := range result.Tools {
			if td.URL, err = resolve(td.URL); err != nil {
				return Config{}, err
			}
			resolved[k] = td
		}
		result.Tools = resolved
	}

	return result, nil
}

func resolveMap(m map[string]string, resolve func(string) (string, error)) (map[string]string, error) {
	if len(m) == 0 {
		return m, nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		resolved, err := resolve(v)
		if err != nil {
			return nil, err
		}
		result[k] = resolved
	}
	return result, nil
}

func resolveSlice(s []string, resolve func(string) (string, error)) ([]string, error) {
	if len(s) == 0 {
		return s, nil
	}
	result := make([]string, len(s))
	for i, v := range s {
		resolved, err := resolve(v)
		if err != nil {
			return nil, err
		}
		result[i] = resolved
	}
	return result, nil
}
