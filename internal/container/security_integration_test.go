package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildSpec_RejectsTraversalInVolumes(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Volumes = []string{"/home/../etc/passwd:/etc/passwd"}

	// BuildSpec should either skip the volume or the caller should validate
	// For now, test that ValidateConfigVolumes catches it
	errs := ValidateConfigVolumes(p.Config.Volumes)
	assert.NotEmpty(t, errs, "path traversal in volumes should be caught")
}

func TestBuildSpec_RejectsSensitiveVolumePaths(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Volumes = []string{"/etc:/mounted-etc"}

	errs := ValidateConfigVolumes(p.Config.Volumes)
	assert.NotEmpty(t, errs, "/etc should be blocked")
}

func TestBuildSpec_AllowsDockerSocket(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Volumes = []string{"/var/run/docker.sock:/var/run/docker.sock"}

	errs := ValidateConfigVolumes(p.Config.Volumes)
	assert.Empty(t, errs, "docker socket should be allowed")
}
