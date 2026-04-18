package workspace

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashPath_Deterministic(t *testing.T) {
	h1 := HashPath("myhost", "/home/user/projects/myapp")
	h2 := HashPath("myhost", "/home/user/projects/myapp")
	assert.Equal(t, h1, h2)
}

func TestHashPath_Length(t *testing.T) {
	h := HashPath("myhost", "/home/user/projects/myapp")
	assert.Len(t, h, 12)
}

func TestHashPath_DifferentPaths(t *testing.T) {
	h1 := HashPath("myhost", "/home/user/project-a")
	h2 := HashPath("myhost", "/home/user/project-b")
	assert.NotEqual(t, h1, h2)
}

func TestHashPath_DifferentHosts(t *testing.T) {
	h1 := HashPath("host-a", "/home/user/project")
	h2 := HashPath("host-b", "/home/user/project")
	assert.NotEqual(t, h1, h2)
}

func TestHashPath_HexCharacters(t *testing.T) {
	h := HashPath("myhost", "/some/path")
	for _, c := range h {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"hash should contain only hex characters, got %c", c)
	}
}

func TestContainerWorkdir(t *testing.T) {
	w := ContainerWorkdir("myhost", "/home/user/projects/myapp")
	assert.Contains(t, w, "/workspace/")
	assert.Len(t, w, len("/workspace/")+12)
}

func TestHashPath_EmptyHostname(t *testing.T) {
	h1 := HashPath("", "/path")
	h2 := HashPath("", "/path")
	assert.Equal(t, h1, h2, "empty hostname should still be deterministic")
	assert.Len(t, h1, 12)
}

func TestHashPath_EmptyPath(t *testing.T) {
	h := HashPath("host", "")
	assert.Len(t, h, 12)
}

