package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const version = "dev"

func main() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: cannot determine executable path: %v\n", err)
		os.Exit(1)
	}

	name := filepath.Base(exe)

	if name == "ai-shim" || name == "ai-shim.exe" {
		// Direct invocation — management mode
		if err := runManage(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Symlink invocation — agent launch mode
	if err := runAgent(name, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: %v\n", err)
		os.Exit(1)
	}
}

func runManage(args []string) error {
	if len(args) == 0 || args[0] == "version" {
		fmt.Printf("ai-shim %s\n", version)
		return nil
	}
	return fmt.Errorf("unknown command: %s", args[0])
}

func runAgent(name string, args []string) error {
	return fmt.Errorf("agent launch not yet implemented (invoked as %q with args %v)", name, args)
}
