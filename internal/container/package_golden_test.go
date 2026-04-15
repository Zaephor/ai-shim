package container

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updatePackageGolden is a package-local flag so it doesn't collide with the
// install package's -update-golden. Run:
//
//	go test ./internal/container/ -run PackageScript_Golden -update-golden
var updatePackageGolden = flag.Bool("update-golden", false, "update golden files")

// packageGoldenCases covers the three branches of generatePackageScript:
//   - packages_empty:               no packages → empty string
//   - packages_single:              one simple package
//   - packages_multi_with_quoting:  multiple packages with one needing
//     shell.Quote (shell-special chars)
var packageGoldenCases = map[string][]string{
	"packages_empty":              nil,
	"packages_single":             {"curl"},
	"packages_multi_with_quoting": {"curl", "git", "weird; name"},
}

// TestPackageScript_Golden compares the package installation script against
// golden files in testdata/. If the output changes, re-run with
// -update-golden to regenerate.
func TestPackageScript_Golden(t *testing.T) {
	for name, packages := range packageGoldenCases {
		t.Run(name, func(t *testing.T) {
			got := generatePackageScript(packages)
			goldenPath := filepath.Join("testdata", name+".golden.sh")

			if *updatePackageGolden {
				require.NoError(t, os.MkdirAll("testdata", 0755))
				require.NoError(t, os.WriteFile(goldenPath, []byte(got), 0644))
				t.Logf("updated %s", goldenPath)
				return
			}

			want, err := os.ReadFile(goldenPath)
			if os.IsNotExist(err) {
				t.Fatalf("golden file %s not found — run with -update-golden to create it", goldenPath)
			}
			require.NoError(t, err)

			assert.Equal(t, string(want), got,
				"package script output changed — if intentional, run: go test ./internal/container/ -run PackageScript_Golden -update-golden")
		})
	}
}
