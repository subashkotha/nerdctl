package container

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/containerd/nerdctl/mod/tigron/expect"
	"github.com/containerd/nerdctl/mod/tigron/test"
	"github.com/containerd/nerdctl/v2/pkg/cmd/container"
	"github.com/containerd/nerdctl/v2/pkg/testutil"
	"github.com/containerd/nerdctl/v2/pkg/testutil/nerdtest"
)

func TestContainerHealthCheck(t *testing.T) {
	testCase := nerdtest.Setup()
	testCase.SubTests = []*test.Case{
		{
			Description: "Basic health check functionality",
			SubTests: []*test.Case{
				{
					Description: "Container does not exist",
					Command:     test.Command("container", "healthcheck", "non-existent"),
					Expected:    test.Expects(1, []error{errors.New("no such container non-existent")}, nil),
				},
				{
					Description: "Basic health check success",
					Setup: func(data test.Data, helpers test.Helpers) {
						containerName := data.Identifier()
						helpers.Ensure("run", "-d", "--name", containerName,
							"--label", "healthcheck/config={\"Test\":[\"/bin/sh\",\"-c\",\"echo health-ok\"],\"Interval\":1000000000,\"Timeout\":5000000000}",
							testutil.CommonImage, "sleep", nerdtest.Infinity)
						data.Set("containerName", containerName)
					},
					Cleanup: func(data test.Data, helpers test.Helpers) {
						helpers.Anyhow("rm", "-f", data.Get("containerName"))
					},
					Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
						return helpers.Command("container", "healthcheck", data.Get("containerName"))
					},
					Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
						return &test.Expected{
							ExitCode: 0,
							Errors:   nil,
							Output:   expect.Contains("health-ok"),
						}
					},
				},
			},
		},
		{
			Description: "Health check with different container states",
			SubTests: []*test.Case{
				{
					Description: "Health check without task",
					Setup: func(data test.Data, helpers test.Helpers) {
						containerName := data.Identifier()
						helpers.Ensure("create", "--name", containerName,
							"--label", "healthcheck/config={\"Test\":[\"/bin/hostname\"],\"Interval\":1000000000,\"Timeout\":5000000000}",
							testutil.CommonImage, "sleep", nerdtest.Infinity)
						data.Set("containerName", containerName)
					},
					Cleanup: func(data test.Data, helpers test.Helpers) {
						helpers.Anyhow("rm", "-f", data.Get("containerName"))
					},
					Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
						return helpers.Command("container", "healthcheck", data.Get("containerName"))
					},
					Expected: test.Expects(1, []error{errors.New("failed to get container task: no running task found")}, nil),
				},
				{
					Description: "Health check on stopped container",
					Setup: func(data test.Data, helpers test.Helpers) {
						containerName := data.Identifier()
						helpers.Ensure("run", "-d", "--name", containerName,
							"--label", "healthcheck/config={\"Test\":[\"/bin/hostname\"],\"Interval\":1000000000,\"Timeout\":5000000000}",
							testutil.CommonImage, "sleep", nerdtest.Infinity)
						helpers.Ensure("stop", "--time=2", containerName)
						data.Set("containerName", containerName)
					},
					Cleanup: func(data test.Data, helpers test.Helpers) {
						helpers.Anyhow("rm", "-f", data.Get("containerName"))
					},
					Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
						return helpers.Command("container", "healthcheck", data.Get("containerName"))
					},
					Expected: test.Expects(1, []error{errors.New("container is not running (status: stopped)")}, nil),
				},
			},
		},
		{
			Description: "Health check configuration variations",
			SubTests: []*test.Case{
				{
					Description: "Missing health check config",
					Setup: func(data test.Data, helpers test.Helpers) {
						containerName := data.Identifier()
						helpers.Ensure("run", "-d", "--name", containerName, testutil.CommonImage, "sleep", nerdtest.Infinity)
						data.Set("containerName", containerName)
					},
					Cleanup: func(data test.Data, helpers test.Helpers) {
						helpers.Anyhow("rm", "-f", data.Get("containerName"))
					},
					Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
						return helpers.Command("container", "healthcheck", data.Get("containerName"))
					},
					Expected: test.Expects(1, []error{errors.New("container has no health check configured")}, nil),
				},
				{
					Description: "Health check with CMD format",
					Setup: func(data test.Data, helpers test.Helpers) {
						containerName := data.Identifier()
						helpers.Ensure("run", "-d", "--name", containerName,
							"--label", "healthcheck/config={\"Test\":[\"CMD\",\"/bin/sh\",\"-c\",\"echo health-ok\"],\"Interval\":1000000000,\"Timeout\":5000000000}",
							testutil.CommonImage, "sleep", nerdtest.Infinity)
						data.Set("containerName", containerName)
					},
					Cleanup: func(data test.Data, helpers test.Helpers) {
						helpers.Anyhow("rm", "-f", data.Get("containerName"))
					},
					Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
						return helpers.Command("container", "healthcheck", data.Get("containerName"))
					},
					Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
						return &test.Expected{
							ExitCode: 0,
							Errors:   nil,
							Output:   expect.Contains("health-ok"),
						}
					},
				},
				{
					Description: "Health check with CMD-SHELL format",
					Setup: func(data test.Data, helpers test.Helpers) {
						containerName := data.Identifier()
						helpers.Ensure("run", "-d", "--name", containerName,
							"--label", "healthcheck/config={\"Test\":[\"CMD-SHELL\",\"echo health-ok\"],\"Interval\":1000000000,\"Timeout\":5000000000}",
							testutil.CommonImage, "sleep", nerdtest.Infinity)
						data.Set("containerName", containerName)
					},
					Cleanup: func(data test.Data, helpers test.Helpers) {
						helpers.Anyhow("rm", "-f", data.Get("containerName"))
					},
					Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
						return helpers.Command("container", "healthcheck", data.Get("containerName"))
					},
					Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
						return &test.Expected{
							ExitCode: 0,
							Errors:   nil,
							Output:   expect.Contains("health-ok"),
						}
					},
				},
				{
					Description: "Health check with direct command array",
					Setup: func(data test.Data, helpers test.Helpers) {
						containerName := data.Identifier()
						helpers.Ensure("run", "-d", "--name", containerName,
							"--label", "healthcheck/config={\"Test\":[\"/bin/sh\",\"-c\",\"echo health-ok\"],\"Interval\":1000000000,\"Timeout\":5000000000}",
							testutil.CommonImage, "sleep", nerdtest.Infinity)
						data.Set("containerName", containerName)
					},
					Cleanup: func(data test.Data, helpers test.Helpers) {
						helpers.Anyhow("rm", "-f", data.Get("containerName"))
					},
					Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
						return helpers.Command("container", "healthcheck", data.Get("containerName"))
					},
					Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
						return &test.Expected{
							ExitCode: 0,
							Errors:   nil,
							Output:   expect.Contains("health-ok"),
						}
					},
				},
			},
		},
		{
			Description: "Advanced health check scenarios",
			SubTests: []*test.Case{
				{
					Description: "Health check times out due to long-running command",
					Setup: func(data test.Data, helpers test.Helpers) {
						containerName := data.Identifier()
						// Run a health check that sleeps for 10s, but timeout is set to 2s
						helpers.Ensure("run", "-d", "--name", containerName,
							"--label", `healthcheck/config={"Test":["CMD-SHELL","sh -c 'sleep 10'"],"Interval":1000000000,"Timeout":2000000000}`,
							testutil.CommonImage, "sleep", nerdtest.Infinity)
						data.Set("containerName", containerName)
					},
					Cleanup: func(data test.Data, helpers test.Helpers) {
						helpers.Anyhow("rm", "-f", data.Get("containerName"))
					},
					Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
						return helpers.Command("container", "healthcheck", data.Get("containerName"))
					},
					Expected: test.Expects(1, nil, func(stdout, info string, t *testing.T) {
						if !strings.Contains(info, "health check timed out after 2s") {
							t.Errorf("Expected stderr to contain timeout message, got: %s", info)
						}
					}),
				},
				{
					Description: "Health check with retries",
					Setup: func(data test.Data, helpers test.Helpers) {
						containerName := data.Identifier()
						helpers.Ensure("run", "-d", "--name", containerName,
							"--label", "healthcheck/config={\"Test\":[\"CMD-SHELL\",\"exit 1\"],\"Interval\":1000000000,\"Timeout\":1000000000,\"Retries\":2}",
							testutil.CommonImage, "sleep", nerdtest.Infinity)
						data.Set("containerName", containerName)
					},
					Cleanup: func(data test.Data, helpers test.Helpers) {
						helpers.Anyhow("rm", "-f", data.Get("containerName"))
					},
					Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
						containerName := data.Get("containerName")

						// First health check
						cmd := helpers.Command("container", "healthcheck", containerName)
						cmd.Run(nil)
						verifyHealthStatus(helpers, containerName, "healthy", 1)

						// Second health check
						cmd = helpers.Command("container", "healthcheck", containerName)
						cmd.Run(nil)
						verifyHealthStatus(helpers, containerName, "unhealthy", 2)

						return cmd
					},
					Expected: test.Expects(1, nil, nil),
				},
				// {
				// 	Description: "Health check with environment variables",
				// 	Setup: func(data test.Data, helpers test.Helpers) {
				// 		containerName := data.Identifier()
				// 		helpers.Ensure("run", "-d", "--name", containerName,
				// 			"--env", "HEALTHCHECK_VAR=test",
				// 			"--label", "healthcheck/config={\"Test\":[\"CMD-SHELL\",\"echo \\\\$HEALTHCHECK_VAR\"],\"Interval\":1000000000,\"Timeout\":1000000000}",
				// 			testutil.CommonImage, "sleep", nerdtest.Infinity)
				// 		data.Set("containerName", containerName)
				// 	},
				// 	Cleanup: func(data test.Data, helpers test.Helpers) {
				// 		helpers.Anyhow("rm", "-f", data.Get("containerName"))
				// 	},
				// 	Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				// 		return helpers.Command("container", "healthcheck", data.Get("containerName"))
				// 	},
				// 	Expected: test.Expects(0, nil, expect.Contains("test")),
				// },
				{
					Description: "Health check respects container WorkingDir",
					Setup: func(data test.Data, helpers test.Helpers) {
						containerName := data.Identifier()
						helpers.Ensure("run", "-d", "--name", containerName,
							"--workdir", "/tmp",
							"--label", "healthcheck/config={\"Test\":[\"CMD-SHELL\",\"pwd\"],\"Interval\":1000000000,\"Timeout\":1000000000}",
							testutil.CommonImage, "sleep", nerdtest.Infinity)
						data.Set("containerName", containerName)
					},
					Cleanup: func(data test.Data, helpers test.Helpers) {
						helpers.Anyhow("rm", "-f", data.Get("containerName"))
					},
					Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
						return helpers.Command("container", "healthcheck", data.Get("containerName"))
					},
					Expected: test.Expects(0, nil, expect.Contains("/tmp")),
				},
				{
					Description: "Invalid health check command",
					Setup: func(data test.Data, helpers test.Helpers) {
						containerName := data.Identifier()
						helpers.Ensure("run", "-d", "--name", containerName,
							"--label", "healthcheck/config={\"Test\":[\"CMD-SHELL\",\"\"],\"Interval\":1000000000,\"Timeout\":1000000000}",
							testutil.CommonImage, "sleep", nerdtest.Infinity)
						data.Set("containerName", containerName)
					},
					Cleanup: func(data test.Data, helpers test.Helpers) {
						helpers.Anyhow("rm", "-f", data.Get("containerName"))
					},
					Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
						return helpers.Command("container", "healthcheck", data.Get("containerName"))
					},
					Expected: test.Expects(1, []error{errors.New("no health check command specified")}, nil),
				},
			},
		},
	}
	testCase.Run(t)
}

// verifyHealthStatus checks the container's health status and failing streak
func verifyHealthStatus(helpers test.Helpers, containerName string, expectedStatus string, expectedStreak int) {
	inspect := helpers.Capture("container", "inspect", containerName)
	var containers []struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if err := json.Unmarshal([]byte(inspect), &containers); err != nil {
		helpers.T().Fatalf("failed to unmarshal container inspect: %v", err)
	}
	if len(containers) != 1 {
		helpers.T().Fatalf("expected 1 container, got %d", len(containers))
	}

	statusJSON := containers[0].Config.Labels[container.HealthStatusLabel]
	var healthStatus container.HealthStatus
	helpers.T().Logf("health status: %s", statusJSON)
	if err := json.Unmarshal([]byte(statusJSON), &healthStatus); err != nil {
		helpers.T().Fatalf("failed to unmarshal health status: %v", err)
	}

	if healthStatus.Status != expectedStatus {
		helpers.T().Fatalf("expected status %s, got %s", expectedStatus, healthStatus.Status)
	}
	if healthStatus.FailingStreak != expectedStreak {
		helpers.T().Fatalf("expected failing streak %d, got %d", expectedStreak, healthStatus.FailingStreak)
	}
}
