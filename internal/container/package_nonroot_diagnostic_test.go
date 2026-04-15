package container

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratePackageScript_NonRootDiagnostic executes the generated package
// install snippet under /bin/sh with a scrubbed PATH (no sudo) and a non-zero
// UID. Verifies the fail branch fires, exits 1, and stderr carries the
// documented "Options:" hint and the declared package names.
//
// This complements package_golden_test.go, which snapshots the static script
// text: this test actually *runs* the branch to prove the diagnostic is
// emitted as intended when neither root nor passwordless sudo is available.
func TestGeneratePackageScript_NonRootDiagnostic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX /bin/sh")
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skipf("/bin/sh not available: %v", err)
	}
	if syscall.Geteuid() == 0 {
		t.Skip("cannot simulate non-root path when already root")
	}

	pkgs := []string{"curl", "git"}
	script := generatePackageScript(pkgs)
	require.NotEmpty(t, script, "generatePackageScript must produce output for non-empty list")

	// Write to temp file.
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "install.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o700))

	// Empty PATH dir so `command -v sudo` fails even if the host has sudo.
	emptyBin := filepath.Join(dir, "emptybin")
	require.NoError(t, os.Mkdir(emptyBin, 0o755))

	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.Env = []string{
		"PATH=" + emptyBin,
		"HOME=" + dir,
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.Error(t, err, "script must fail when neither root nor passwordless sudo is available")

	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok, "expected *exec.ExitError, got %T: %v", err, err)
	assert.Equal(t, 1, exitErr.ExitCode(), "fail-branch must exit 1")

	stderrStr := stderr.String()
	// Core diagnostic assertions — these are the load-bearing bits of the
	// user-visible message at commit 7de73f4.
	assert.Contains(t, stderrStr, "ERROR: profile requests apt packages",
		"must name the failure mode")
	assert.Contains(t, stderrStr, "without passwordless sudo",
		"must explain why installation is impossible")
	assert.Contains(t, stderrStr, "apt-get requires root. Options:",
		"must surface the Options: hint header")
	assert.Contains(t, stderrStr, "use a base image that runs as root",
		"must list base-image-as-root remediation")
	assert.Contains(t, stderrStr, "passwordless sudo to this user",
		"must list passwordless-sudo remediation")
	assert.Contains(t, stderrStr, "rewrite these deps as self-contained tools",
		"must list tools-entry remediation")
	assert.Contains(t, stderrStr, "binary-download / tar-extract / custom",
		"must enumerate available tool entry types")

	// Declared package names must appear in the diagnostic so users know
	// which profile packages triggered the failure.
	for _, pkg := range pkgs {
		assert.Contains(t, stderrStr, pkg,
			"diagnostic should name package %q so user can correlate to profile", pkg)
	}

	// Sanity: the announcement line ("Installing packages: ...") lands on
	// stdout before the branch is chosen.
	assert.True(t,
		strings.Contains(stdout.String(), "Installing packages:") ||
			strings.Contains(stderrStr, "Installing packages:"),
		"script should announce which packages it attempts to install")
}
