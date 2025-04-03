package container

import (
	"testing"

	"github.com/containerd/nerdctl/v2/testutil"
)

func TestHealthCheckCommand(t *testing.T) {
	base := testutil.NewBase(t)

	// Test non-existent container
	result := base.Cmd("container", "healthcheck", "non-existent").Run()
	result.AssertFail()

	// Create a container with health check configuration via labels
	containerName := testutil.Identifier(t)
	base.Cmd("run", "-d", "--name", containerName,
		"--label", "healthcheck/config={\"Test\":[\"/bin/hostname\"],\"Interval\":1000000000,\"Timeout\":5000000000}",
		"busybox", "sleep", "infinity").AssertOK()

	// Test health check command
	base.Cmd("container", "healthcheck", containerName).AssertOK()
}

// TODO: Add more test cases
// func TestHealthCheckCommandWithTimeout(t *testing.T) {
// 	base := testutil.NewBase(t)
// 	containerName := testutil.Identifier(t)
// 	base.Cmd("run", "-d", "--name", containerName,
// 		"--label", "healthcheck/config={\"Test\":[\"CMD-SHELL\",\"sleep 5\"],\"Interval\":1000000000,\"Timeout\":1000000000}",
// 		"busybox", "sleep", "infinity").AssertOK()
// 	base.Cmd("container", "healthcheck", containerName).AssertFail()
// }

// func TestHealthCheckCommandWithRetries(t *testing.T) {
// 	base := testutil.NewBase(t)
// 	containerName := testutil.Identifier(t)
// 	base.Cmd("run", "-d", "--name", containerName,
// 		"--label", "healthcheck/config={\"Test\":[\"CMD-SHELL\",\"exit 1\"],\"Interval\":1000000000,\"Timeout\":1000000000,\"Retries\":2}",
// 		"busybox", "sleep", "infinity").AssertOK()
// 	base.Cmd("container", "healthcheck", containerName).AssertFail()
// }
