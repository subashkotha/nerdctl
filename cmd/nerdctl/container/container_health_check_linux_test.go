package container

import (
	"errors"
	"testing"

	"github.com/containerd/nerdctl/mod/tigron/expect"
	"github.com/containerd/nerdctl/mod/tigron/test"
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
				// {
				// 	Description: "Health check with timeout",
				// 	Setup: func(data test.Data, helpers test.Helpers) {
				// 		containerName := data.Identifier()
				// 		helpers.Ensure("run", "-d", "--name", containerName,
				// 			"--label", "healthcheck/config={\"Test\":[\"CMD-SHELL\",\"sleep 5\"],\"Interval\":1000000000,\"Timeout\":1000000000}",
				// 			testutil.CommonImage, "sleep", nerdtest.Infinity)
				// 		data.Set("containerName", containerName)
				// 	},
				// 	Cleanup: func(data test.Data, helpers test.Helpers) {
				// 		helpers.Anyhow("rm", "-f", data.Get("containerName"))
				// 	},
				// 	Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
				// 		return helpers.Command("container", "healthcheck", data.Get("containerName"))
				// 	},
				// 	Expected: test.Expects(1, nil, nil),
				// },
			},
		},
		// {
		// 	Description: "Advanced health check scenarios",
		// 	SubTests: []*test.Case{
		// 		{
		// 			Description: "Health check with retries",
		// 			Setup: func(data test.Data, helpers test.Helpers) {
		// 				containerName := data.Identifier()
		// 				helpers.Ensure("run", "-d", "--name", containerName,
		// 					"--label", "healthcheck/config={\"Test\":[\"CMD-SHELL\",\"exit 1\"],\"Interval\":1000000000,\"Timeout\":1000000000,\"Retries\":2}",
		// 					testutil.CommonImage, "sleep", nerdtest.Infinity)
		// 				data.Set("containerName", containerName)
		// 			},
		// 			Cleanup: func(data test.Data, helpers test.Helpers) {
		// 				helpers.Anyhow("rm", "-f", data.Get("containerName"))
		// 			},
		// 			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
		// 				return helpers.Command("container", "healthcheck", data.Get("containerName"))
		// 			},
		// 			Expected: test.Expects(1, nil, nil),
		// 		},
		// 		{
		// 			Description: "Health check with shell command",
		// 			Setup: func(data test.Data, helpers test.Helpers) {
		// 				containerName := data.Identifier()
		// 				helpers.Ensure("run", "-d", "--name", containerName,
		// 					"--label", "healthcheck/config={\"Test\":[\"CMD-SHELL\",\"test -f /etc/hostname\"],\"Interval\":1000000000,\"Timeout\":5000000000}",
		// 					testutil.CommonImage, "sleep", nerdtest.Infinity)
		// 				data.Set("containerName", containerName)
		// 			},
		// 			Cleanup: func(data test.Data, helpers test.Helpers) {
		// 				helpers.Anyhow("rm", "-f", data.Get("containerName"))
		// 			},
		// 			Command: func(data test.Data, helpers test.Helpers) test.TestableCommand {
		// 				return helpers.Command("container", "healthcheck", data.Get("containerName"))
		// 			},
		// 			Expected: test.Expects(0, nil, nil),
		// 		},
		// 	},
		// },
	}
	testCase.Run(t)
}
