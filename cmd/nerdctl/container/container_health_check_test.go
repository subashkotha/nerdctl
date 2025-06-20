/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package container

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/containerd/nerdctl/v2/pkg/inspecttypes/dockercompat"
	"strconv"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/containerd/nerdctl/mod/tigron/expect"
	"github.com/containerd/nerdctl/mod/tigron/require"
	"github.com/containerd/nerdctl/mod/tigron/test"

	"github.com/containerd/nerdctl/v2/pkg/healthcheck"
	"github.com/containerd/nerdctl/v2/pkg/testutil"
	"github.com/containerd/nerdctl/v2/pkg/testutil/nerdtest"
)

func TestContainerHealthCheckBasic(t *testing.T) {
	testCase := nerdtest.Setup()

	// Docker CLI does not provide a standalone healthcheck command.
	testCase.Require = require.Not(nerdtest.Docker)

	testCase.SubTests = []*test.Case{
		{
			Description: "Container does not exist",
			Command:     test.Command("container", "healthcheck", "non-existent"),
			Expected:    test.Expects(1, []error{errors.New("no such container non-existent")}, nil),
		},
		{
			Description: "Missing health check config",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				return helpers.Command("container", "healthcheck", data.Identifier())
			},
			Expected: test.Expects(1, []error{errors.New("container has no health check configured")}, nil),
		},
		{
			Description: "Basic health check success",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--health-cmd", "echo healthy",
					"--health-interval", "45s",
					"--health-timeout", "30s",
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				return helpers.Command("container", "healthcheck", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(stdout, _ string, t *testing.T) {
						inspect := nerdtest.InspectContainer(helpers, data.Identifier())
						h := inspect.State.Health
						assert.Assert(t, h != nil, "expected health state to be present")
						assert.Equal(t, healthcheck.Healthy, h.Status)
						assert.Equal(t, 0, h.FailingStreak)
						assert.Assert(t, len(h.Log) > 0, "expected at least one health check log entry")
					}),
				}
			},
		},
		{
			Description: "Health check on stopped container",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--health-cmd", "echo healthy",
					"--health-interval", "3s",
					testutil.CommonImage, "sleep", "2")
				helpers.Ensure("stop", data.Identifier())
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				return helpers.Command("container", "healthcheck", data.Identifier())
			},
			Expected: test.Expects(1, []error{errors.New("container is not running (status: stopped)")}, nil),
		},
		{
			Description: "Health check without task",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("create", "--name", data.Identifier(),
					"--health-cmd", "echo healthy",
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				return helpers.Command("container", "healthcheck", data.Identifier())
			},
			Expected: test.Expects(1, []error{errors.New("failed to get container task: no running task found")}, nil),
		},
	}

	testCase.Run(t)
}

func TestContainerHealthCheckAdvance(t *testing.T) {
	testCase := nerdtest.Setup()

	// Docker CLI does not provide a standalone healthcheck command.
	testCase.Require = require.Not(nerdtest.Docker)

	testCase.SubTests = []*test.Case{
		{
			Description: "Health check timeout scenario",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--health-cmd", "sleep 10",
					"--health-timeout", "2s",
					"--health-interval", "1s",
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				return helpers.Command("container", "healthcheck", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(stdout, _ string, t *testing.T) {
						inspect := nerdtest.InspectContainer(helpers, data.Identifier())
						h := inspect.State.Health
						assert.Assert(t, h != nil, "expected health state")
						assert.Equal(t, h.FailingStreak, 1)
						assert.Assert(t, len(inspect.State.Health.Log) > 0, "expected health log to have entries")
						last := inspect.State.Health.Log[0]
						assert.Equal(t, -1, last.ExitCode)
					}),
				}
			},
		},
		{
			Description: "Health check failing streak behavior",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--health-cmd", "exit 1",
					"--health-interval", "1s",
					"--health-retries", "2",
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				// Run healthcheck twice to ensure failing streak
				for i := 0; i < 2; i++ {
					helpers.Ensure("container", "healthcheck", data.Identifier())
					time.Sleep(2 * time.Second)
				}
				return helpers.Command("inspect", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(stdout, _ string, t *testing.T) {
						inspect := nerdtest.InspectContainer(helpers, data.Identifier())
						h := inspect.State.Health
						assert.Assert(t, h != nil, "expected health state")
						assert.Equal(t, h.Status, healthcheck.Unhealthy)
						assert.Equal(t, h.FailingStreak, 2)
					}),
				}
			},
		},
		{
			Description: "Health check with start period",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--health-cmd", "exit 1",
					"--health-interval", "1s",
					"--health-start-period", "5s",
					"--health-retries", "2",
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				return helpers.Command("container", "healthcheck", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(stdout, _ string, t *testing.T) {
						inspect := nerdtest.InspectContainer(helpers, data.Identifier())
						h := inspect.State.Health
						assert.Assert(t, h != nil, "expected health state")
						assert.Equal(t, h.Status, healthcheck.Starting)
						assert.Equal(t, h.FailingStreak, 0)
					}),
				}
			},
		},
		{
			Description: "Health check with invalid command",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--health-cmd", "not-a-real-cmd",
					"--health-interval", "1s",
					"--health-retries", "1",
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				return helpers.Command("container", "healthcheck", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(stdout, _ string, t *testing.T) {
						inspect := nerdtest.InspectContainer(helpers, data.Identifier())
						h := inspect.State.Health
						assert.Assert(t, h != nil, "expected health state")
						assert.Equal(t, h.Status, healthcheck.Unhealthy)
						assert.Equal(t, h.FailingStreak, 1)
					}),
				}
			},
		},
		{
			Description: "No healthcheck flag disables health status",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--no-healthcheck", testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				return helpers.Command("inspect", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(stdout, _ string, t *testing.T) {
						inspect := nerdtest.InspectContainer(helpers, data.Identifier())
						assert.Assert(t, inspect.State.Health == nil, "expected health to be nil with --no-healthcheck")
					}),
				}
			},
		},
		{
			Description: "Healthcheck using CMD-SHELL format",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--health-cmd", "echo shell-format", "--health-interval", "1s",
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				return helpers.Command("container", "healthcheck", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(_, _ string, t *testing.T) {
						inspect := nerdtest.InspectContainer(helpers, data.Identifier())
						h := inspect.State.Health
						assert.Assert(t, h != nil, "expected health state")
						assert.Equal(t, h.Status, healthcheck.Healthy)
						assert.Assert(t, len(h.Log) > 0)
						assert.Assert(t, strings.Contains(h.Log[0].Output, "shell-format"))
					}),
				}
			},
		},
		{
			Description: "Health check uses container environment variables",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--env", "MYVAR=test-value",
					"--health-cmd", "echo $MYVAR",
					"--health-interval", "1s",
					"--health-timeout", "1s",
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				return helpers.Command("container", "healthcheck", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(stdout, _ string, t *testing.T) {
						inspect := nerdtest.InspectContainer(helpers, data.Identifier())
						h := inspect.State.Health
						assert.Assert(t, h != nil, "expected health state")
						assert.Equal(t, h.Status, healthcheck.Healthy)
						assert.Assert(t, h.FailingStreak == 0)
						assert.Assert(t, strings.Contains(h.Log[0].Output, "test"), "expected health log output to contain 'test'")
					}),
				}
			},
		},
		{
			Description: "Health check respects container WorkingDir",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--workdir", "/tmp",
					"--health-cmd", "pwd",
					"--health-interval", "1s",
					"--health-timeout", "1s",
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				return helpers.Command("container", "healthcheck", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(stdout, _ string, t *testing.T) {
						inspect := nerdtest.InspectContainer(helpers, data.Identifier())
						h := inspect.State.Health
						assert.Assert(t, h != nil, "expected health state")
						assert.Equal(t, h.Status, healthcheck.Healthy)
						assert.Equal(t, h.FailingStreak, 0)
						assert.Assert(t, strings.Contains(h.Log[0].Output, "/tmp"), "expected health log output to contain '/tmp'")
					}),
				}
			},
		},
		{
			Description: "Healthcheck emits large output repeatedly",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--health-cmd", "yes X | head -c 60000",
					"--health-interval", "1s", "--health-timeout", "2s",
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				for i := 0; i < 3; i++ {
					helpers.Ensure("container", "healthcheck", data.Identifier())
					time.Sleep(2 * time.Second)
				}
				return helpers.Command("inspect", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(_, _ string, t *testing.T) {
						inspect := nerdtest.InspectContainer(helpers, data.Identifier())
						h := inspect.State.Health
						assert.Assert(t, h != nil, "expected health state")
						assert.Equal(t, h.Status, healthcheck.Healthy)
						assert.Assert(t, len(h.Log) >= 3, "expected at least 3 health log entries")
						for _, log := range h.Log {
							assert.Assert(t, len(log.Output) >= 1024, "each output should be >= 1024 bytes")
						}
					}),
				}
			},
		},
		{
			Description: "Health log in inspect keeps only the latest 5 entries",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--health-cmd", "exit 1",
					"--health-interval", "1s",
					"--health-retries", "1",
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				for i := 0; i < 7; i++ {
					helpers.Ensure("container", "healthcheck", data.Identifier())
					time.Sleep(1 * time.Second)
				}
				return helpers.Command("inspect", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(_, _ string, t *testing.T) {
						inspect := nerdtest.InspectContainer(helpers, data.Identifier())
						h := inspect.State.Health
						assert.Assert(t, h != nil, "expected health state")
						assert.Equal(t, h.Status, healthcheck.Unhealthy)
						assert.Assert(t, len(h.Log) <= 5, "expected health log to contain at most 5 entries")
					}),
				}
			},
		},
		{
			Description: "Healthcheck with large output gets truncated in health log",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--health-cmd", "yes X | head -c 1048576", // 1MB output
					"--health-interval", "1s", "--health-timeout", "2s",
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				return helpers.Command("container", "healthcheck", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(_, _ string, t *testing.T) {
						inspect := nerdtest.InspectContainer(helpers, data.Identifier())
						h := inspect.State.Health
						assert.Assert(t, h != nil, "expected health state")
						assert.Equal(t, h.Status, healthcheck.Healthy)
						assert.Equal(t, h.FailingStreak, 0)
						assert.Assert(t, len(h.Log) == 1, "expected one log entry")
						output := h.Log[0].Output
						assert.Assert(t, strings.HasSuffix(output, "[truncated]"), "expected output to be truncated with '[truncated]'")
					}),
				}
			},
		},
		{
			Description: "Health status transitions from healthy to unhealthy after retries",
			Setup: func(data test.Data, helpers test.Helpers) {
				containerName := data.Identifier()
				helpers.Ensure("run", "-d", "--name", containerName,
					"--health-cmd", "exit 1",
					"--health-timeout", "10s",
					"--health-retries", "3",
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				for i := 0; i < 4; i++ {
					helpers.Ensure("container", "healthcheck", data.Identifier())
					time.Sleep(2 * time.Second)
				}
				return helpers.Command("inspect", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(stdout, _ string, t *testing.T) {
						inspect := nerdtest.InspectContainer(helpers, data.Identifier())
						h := inspect.State.Health
						assert.Assert(t, h != nil, "expected health state")
						assert.Equal(t, h.Status, healthcheck.Unhealthy)
						assert.Assert(t, h.FailingStreak >= 3)
					}),
				}
			},
		},
		{
			Description: "Failed healthchecks in start-period do not change status",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--health-cmd", "ls /foo || exit 1", "--health-retries", "2",
					"--health-start-period", "30s", // long enough to stay in "starting"
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				// Run healthcheck 3 times (should still be in start period)
				for i := 0; i < 3; i++ {
					helpers.Ensure("container", "healthcheck", data.Identifier())
					time.Sleep(1 * time.Second)
				}
				return helpers.Command("inspect", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(stdout, _ string, t *testing.T) {
						inspect := nerdtest.InspectContainer(helpers, data.Identifier())
						h := inspect.State.Health
						assert.Assert(t, h != nil, "expected health state")
						assert.Equal(t, h.Status, healthcheck.Starting)
						assert.Equal(t, h.FailingStreak, 0, "failing streak should not increase during start period")
					}),
				}
			},
		},
		{
			Description: "Successful healthcheck in start-period sets status to healthy",
			Setup: func(data test.Data, helpers test.Helpers) {
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--health-cmd", "ls || exit 1", "--health-retries", "2",
					testutil.CommonImage, "sleep", nerdtest.Infinity)
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				helpers.Ensure("container", "healthcheck", data.Identifier())
				time.Sleep(1 * time.Second)
				return helpers.Command("inspect", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(stdout, _ string, t *testing.T) {
						inspect := nerdtest.InspectContainer(helpers, data.Identifier())
						h := inspect.State.Health
						assert.Assert(t, h != nil, "expected health state")
						assert.Equal(t, h.Status, healthcheck.Healthy, "expected healthy status even during start-period")
						assert.Equal(t, h.FailingStreak, 0)
					}),
				}
			},
		},
	}

	testCase.Run(t)
}

func TestHealthCheck_SystemdIntegration_Basic(t *testing.T) {
	testCase := nerdtest.Setup()
	testCase.Require = require.Not(nerdtest.Docker)

	testCase.SubTests = []*test.Case{
		//{
		//	Description: "Basic healthy container with systemd-triggered healthcheck",
		//	Setup: func(data test.Data, helpers test.Helpers) {
		//		helpers.Ensure("run", "-d", "--name", data.Identifier(),
		//			"--health-cmd", "echo healthy",
		//			"--health-interval", "2s",
		//			testutil.CommonImage, "sleep", "30")
		//		// Wait for a couple of healthchecks to execute
		//		time.Sleep(5 * time.Second)
		//	},
		//	Cleanup: func(data test.Data, helpers test.Helpers) {
		//		helpers.Anyhow("rm", "-f", data.Identifier())
		//	},
		//	Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
		//		return &test.Expected{
		//			ExitCode: 0,
		//			Output: expect.All(func(stdout, _ string, t *testing.T) {
		//				inspect := nerdtest.InspectContainer(helpers, data.Identifier())
		//				h := inspect.State.Health
		//				assert.Assert(t, h != nil, "expected health state to be present")
		//				assert.Equal(t, h.Status, "healthy")
		//				assert.Assert(t, len(h.Log) > 0, "expected at least one health check log entry")
		//			}),
		//		}
		//	},
		//},
		//{
		//	Description: "Kill stops healthcheck execution",
		//	Setup: func(data test.Data, helpers test.Helpers) {
		//		helpers.Ensure("run", "-d", "--name", data.Identifier(),
		//			"--health-cmd", "echo healthy",
		//			"--health-interval", "1s",
		//			testutil.CommonImage, "sleep", "30")
		//		time.Sleep(5 * time.Second)               // Wait for at least one health check to execute
		//		helpers.Ensure("kill", data.Identifier()) // Kill the container
		//		time.Sleep(3 * time.Second)               // Wait to allow any potential extra healthchecks (shouldn't happen)
		//	},
		//	Cleanup: func(data test.Data, helpers test.Helpers) {
		//		helpers.Anyhow("rm", "-f", data.Identifier())
		//	},
		//	Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
		//		return &test.Expected{
		//			ExitCode: 0,
		//			Output: expect.All(func(stdout, _ string, t *testing.T) {
		//				inspect := nerdtest.InspectContainer(helpers, data.Identifier())
		//				h := inspect.State.Health
		//				assert.Assert(t, h != nil, "expected health state to be present")
		//				assert.Assert(t, len(h.Log) > 0, "expected at least one health check log entry")
		//
		//				// Get container FinishedAt timestamp
		//				containerEnd, err := time.Parse(time.RFC3339Nano, inspect.State.FinishedAt)
		//				assert.NilError(t, err, "parsing container FinishedAt")
		//
		//				// Assert all healthcheck log start times are before container finished
		//				for _, entry := range h.Log {
		//					assert.NilError(t, err, "parsing healthcheck Start time")
		//					assert.Assert(t, entry.Start.Before(containerEnd), "healthcheck ran after container was killed")
		//				}
		//			}),
		//		}
		//	},
		//},
		{
			Description: "Pause/unpause halts and resumes healthcheck execution",
			Setup: func(data test.Data, helpers test.Helpers) {
				data.Labels().Set("cID", data.Identifier())
				helpers.Ensure("run", "-d", "--name", data.Identifier(),
					"--health-cmd", "echo healthy",
					"--health-interval", "1s",
					testutil.CommonImage, "sleep", "30")
				time.Sleep(4 * time.Second)

				// Inspect using raw command
				helpers.Command("container", "inspect", data.Labels().Get("cID")).
					Run(&test.Expected{
						ExitCode: expect.ExitCodeNoCheck,
						Output: func(stdout string, _ string, t *testing.T) {
							var dc []dockercompat.Container
							err := json.Unmarshal([]byte(stdout), &dc)
							assert.NilError(t, err)
							assert.Equal(t, len(dc), 1)
							h := dc[0].State.Health
							assert.Assert(t, h != nil, "expected health state to be present")
							data.Labels().Set("healthStatus", h.Status)
							data.Labels().Set("logCount", strconv.Itoa(len(h.Log)))
							fmt.Printf("📋 Setup Inspect: Status=%s, LogCount=%s\n", h.Status, strconv.Itoa(len(h.Log)))
						},
					})
			},
			Cleanup: func(data test.Data, helpers test.Helpers) {
				helpers.Anyhow("rm", "-f", data.Identifier())
			},
			Expected: func(data test.Data, helpers test.Helpers) *test.Expected {
				return &test.Expected{
					ExitCode: 0,
					Output: expect.All(func(stdout, _ string, t *testing.T) {
						before := data.Labels().Get("logCountBeforePause")
						after := data.Labels().Get("logCountAfterUnpause")

						beforeCount, _ := strconv.Atoi(before)
						afterCount, _ := strconv.Atoi(after)

						assert.Assert(t, afterCount > beforeCount,
							"expected more healthchecks after unpause (got %d → %d)", beforeCount, afterCount)
					}),
				}
			},
		},
	}

	testCase.Run(t)
}
