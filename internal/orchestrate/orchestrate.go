// Package orchestrate holds the agent-launch preparation pipeline shared by the
// `run` and `manage warm` paths. It resolves configuration, validates inputs,
// and provisions on-disk state up to (but not including) container creation and
// the run/reattach/DIND logic, which remain in the command layer.
package orchestrate

import (
	"context"
	"fmt"
	"os"

	"github.com/Zaephor/ai-shim/internal/agent"
	"github.com/Zaephor/ai-shim/internal/config"
	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/platform"
	"github.com/Zaephor/ai-shim/internal/security"
	"github.com/Zaephor/ai-shim/internal/storage"
)

// UnknownAgentError indicates the requested agent name is not registered. The
// command layer renders the available-agent list alongside it.
type UnknownAgentError struct{ Name string }

func (e *UnknownAgentError) Error() string {
	return fmt.Sprintf("unknown agent: %s", e.Name)
}

// Options tunes the shared preparation to match each caller's existing behavior.
type Options struct {
	// ValidateWorkingDir enables working-directory and config-volume validation.
	// The interactive run path sets this; `manage warm` historically does not.
	ValidateWorkingDir bool
}

// Prepared is the resolved launch context produced by Prepare. Container
// creation, image handling, and the run/reattach/DIND steps are the caller's
// responsibility.
type Prepared struct {
	Config   config.Config
	Agent    agent.Definition
	Platform platform.Info
	Pwd      string
}

// Prepare runs the filesystem/config preparation shared by the run and warm
// paths: it loads custom agents, looks up the agent, detects the platform,
// ensures directories, resolves and validates config, and pre-creates agent
// data for the agent and its allowed peers. It performs no Docker I/O, so it is
// unit-testable against a temporary layout.
func Prepare(layout storage.Layout, agentName, profileName string, opts Options) (*Prepared, error) {
	if customDefs := agent.LoadCustomAgents(layout.ConfigDir); customDefs != nil {
		agent.SetCustomAgents(customDefs)
	}

	agentDef, ok := agent.Lookup(agentName)
	if !ok {
		return nil, &UnknownAgentError{Name: agentName}
	}

	platInfo := platform.Detect()
	if platInfo.UID == 0 {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: running as root (UID 0). Container will run as root.\n")
	}

	if err := layout.EnsureDirectories(agentName, profileName); err != nil {
		return nil, fmt.Errorf("setting up directories: %w", err)
	}

	cfg, err := config.Resolve(layout.ConfigDir, agentName, profileName)
	if err != nil {
		return nil, fmt.Errorf("resolving config: %w", err)
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "ai-shim: config error: %s\n", e)
		}
		return nil, fmt.Errorf("invalid config: %d error(s)", len(errs))
	}

	pwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting working directory: %w", err)
	}

	if opts.ValidateWorkingDir {
		if err := security.ValidateWorkingDirectory(pwd); err != nil {
			return nil, err
		}
		if errs := container.ValidateConfigVolumes(cfg.Volumes); len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, "ai-shim: invalid volume: %v\n", e)
			}
			return nil, fmt.Errorf("invalid volume config: %d error(s)", len(errs))
		}
	}

	// Pre-create agent data dirs/files for correct ownership, including any
	// agents this one is allowed to invoke.
	if err := layout.EnsureAgentData(profileName, agentDef.DataDirs, agentDef.DataFiles); err != nil {
		return nil, fmt.Errorf("setting up agent data: %w", err)
	}
	for _, name := range cfg.AllowAgents {
		if allowed, ok := agent.Lookup(name); ok {
			if err := layout.EnsureAgentData(profileName, allowed.DataDirs, allowed.DataFiles); err != nil {
				return nil, fmt.Errorf("setting up agent data for %s: %w", name, err)
			}
		}
	}

	return &Prepared{Config: cfg, Agent: agentDef, Platform: platInfo, Pwd: pwd}, nil
}

// EnsureImage pulls the configured image and inspects its user/home, falling
// back to /home/user when inspection fails (matching prior behavior).
func EnsureImage(ctx context.Context, runner *container.Runner, cfg config.Config) (string, container.ImageUser, error) {
	image := cfg.GetImage()
	if err := runner.EnsureImage(ctx, image); err != nil {
		return "", container.ImageUser{}, fmt.Errorf("preparing image: %w", err)
	}

	imageUser, err := runner.InspectImageUser(ctx, image)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: could not inspect image user, defaulting to /home/user: %v\n", err)
		imageUser = container.ImageUser{HomeDir: "/home/user", Username: "user"}
	}
	return image, imageUser, nil
}
