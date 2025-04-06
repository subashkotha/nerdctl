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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/nerdctl/v2/pkg/api/types"
	"github.com/containerd/nerdctl/v2/pkg/idgen"
	"github.com/opencontainers/runtime-spec/specs-go"
)

const (
	// Health check status
	Starting  = "starting"
	Healthy   = "healthy"
	Unhealthy = "unhealthy"

	// Label keys for health check
	HealthConfigLabel = "healthcheck/config"
	HealthStatusLabel = "healthcheck/status"

	// Health check command types
	HealthCheckCmdNone  = "NONE"
	HealthCheckCmd      = "CMD"
	HealthCheckCmdShell = "CMD-SHELL"
	HealthCheckTestNone = ""
)

// TODO move these structs to a util file
// HealthConfig represents the health check configuration
type HealthConfig struct {
	Test        []string      `json:"test"`        // Test is the test to perform to check that the container is healthy
	Interval    time.Duration `json:"interval"`    // Interval is the time to wait between checks
	Timeout     time.Duration `json:"timeout"`     // Timeout is the time to wait before considering the check to have hung
	StartPeriod time.Duration `json:"startPeriod"` // StartPeriod is the period for the container to initialize before the health check starts counting retries
	Retries     int           `json:"retries"`     // Retries is the number of consecutive failures needed to consider a container as unhealthy
}

// HealthStatus represents the current health status of a container
type HealthStatus struct {
	Status        string `json:"status"`        // Status is the current health status
	FailingStreak int    `json:"failingStreak"` // FailingStreak is the number of consecutive failures
	Log           []Log  `json:"log"`           // Log contains the last few health check logs
}

// Log represents a single health check execution log
type Log struct {
	Start    time.Time `json:"start"`    // Start is when the health check started
	End      time.Time `json:"end"`      // End is when the health check ended
	ExitCode int       `json:"exitCode"` // ExitCode is the exit code of the health check
	Output   string    `json:"output"`   // Output is the output of the health check
}

// HealthCheck executes the health check command for a container
func HealthCheck(ctx context.Context, client *containerd.Client, container containerd.Container, globalOptions types.GlobalCommandOptions) error {
	// verify container status and get task
	task, err := verifyContainerStatus(ctx, container)
	if err != nil {
		return err
	}

	// Check if container has health check configured
	info, err := container.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get container info: %w", err)
	}
	configJSON, ok := info.Labels[HealthConfigLabel]
	if !ok {
		return fmt.Errorf("container has no health check configured")
	}

	// Parse health check configuration from labels
	var config HealthConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return fmt.Errorf("invalid health check configuration: %w", err)
	}

	// Prepare process spec for health check command
	processSpec, err := prepareProcessSpec(&config, container, ctx)
	if err != nil {
		return err
	}

	// Todo figure out if we can re-use exec lib method
	// fmt.Printf("Health check command: %v\n", processSpec.Args)
	execID := "health-check-" + idgen.TruncateID(idgen.GenerateID())
	var stdoutBuf, stderrBuf bytes.Buffer
	startTime := time.Now()
	process, err := task.Exec(ctx, execID, processSpec, cio.NewCreator(
		cio.WithStreams(nil, &stdoutBuf, &stderrBuf),
	))
	if err != nil {
		return fmt.Errorf("failed to execute health check: %w", err)
	}

	if err := process.Start(ctx); err != nil {
		return fmt.Errorf("failed to start health check: %w", err)
	}

	// Todo finalize, how to handle timeout?
	exitStatusC, err := process.Wait(ctx)
	if err != nil {
		return fmt.Errorf("failed to wait for health check: %w", err)
	}

	select {
	case <-time.After(config.Timeout):
		_ = process.Kill(ctx, syscall.SIGKILL) // Optional: force kill
		healthOutput := strings.TrimSpace(stdoutBuf.String() + stderrBuf.String())
		if err := updateHealthStatus(ctx, container, &config, 1, "health check timed out: "+healthOutput, startTime, time.Now()); err != nil {
			return fmt.Errorf("failed to update health status after timeout: %w", err)
		}
		return fmt.Errorf("health check timed out after %v", config.Timeout)

	case exitStatus := <-exitStatusC:
		code, _, _ := exitStatus.Result()
		healthOutput := strings.TrimSpace(stdoutBuf.String() + stderrBuf.String())
		// Todo confirm if we need to log the output
		fmt.Printf("Health check exit status: %d\n", code)
		fmt.Printf("Health check output:\n%s\n", healthOutput)
		if code != 0 {
			if err := updateHealthStatus(ctx, container, &config, code, healthOutput, startTime, time.Now()); err != nil {
				return fmt.Errorf("failed to update health status: %w", err)
			}
			return fmt.Errorf("health check failed with code %d", code)
		}

		// Success path
		if err := updateHealthStatus(ctx, container, &config, 0, healthOutput, startTime, time.Now()); err != nil {
			return fmt.Errorf("failed to update health status after success: %w", err)
		}
	}

	return nil
}

func verifyContainerStatus(ctx context.Context, container containerd.Container) (containerd.Task, error) {
	// Get container task to check status
	task, err := container.Task(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get container task: %w", err)
	}

	// Check if container is running
	status, err := task.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get container status: %w", err)
	}
	if status.Status != containerd.Running {
		return nil, fmt.Errorf("container is not running (status: %s)", status.Status)
	}

	return task, nil
}

// updateHealthStatus updates the health status based on the health check result
func updateHealthStatus(ctx context.Context, container containerd.Container, config *HealthConfig, exitCode uint32, output string, startTime, endTime time.Time) error {
	// Get current health status
	var healthStatus HealthStatus
	info, err := container.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get container info: %w", err)
	}
	if statusJSON, ok := info.Labels["healthcheck/status"]; ok {
		if err := json.Unmarshal([]byte(statusJSON), &healthStatus); err != nil {
			return fmt.Errorf("invalid health status: %w", err)
		}
	} else {
		healthStatus = HealthStatus{
			Status: Starting,
		}
	}

	// Update health status based on exit code
	if exitCode == 0 {
		healthStatus.Status = Healthy
		healthStatus.FailingStreak = 0
	} else {
		healthStatus.FailingStreak++
		if healthStatus.FailingStreak >= config.Retries {
			healthStatus.Status = Unhealthy
		} else {
			healthStatus.Status = Healthy
		}
	}

	// Add log entry
	healthStatus.Log = append(healthStatus.Log, Log{
		Start:    startTime,
		End:      endTime,
		ExitCode: int(exitCode),
		Output:   output,
	})

	// Keep only last 5 logs
	if len(healthStatus.Log) > 5 {
		healthStatus.Log = healthStatus.Log[len(healthStatus.Log)-5:]
	}

	// Update container labels
	statusJSON, err := json.Marshal(healthStatus)
	if err != nil {
		return fmt.Errorf("failed to marshal health status: %w", err)
	}

	_, err = container.SetLabels(ctx, map[string]string{
		HealthStatusLabel: string(statusJSON),
	})
	if err != nil {
		return fmt.Errorf("failed to update container labels: %w", err)
	}

	return nil
}

// prepareProcessSpec prepares the process spec for health check execution
func prepareProcessSpec(healthConfig *HealthConfig, container containerd.Container, ctx context.Context) (*specs.Process, error) {
	hcCommand := healthConfig.Test
	if len(hcCommand) < 1 {
		return nil, fmt.Errorf("no health check command specified")
	}

	var args []string
	switch hcCommand[0] {
	case HealthCheckTestNone, HealthCheckCmdNone:
		return nil, fmt.Errorf("no health check defined")
	case HealthCheckCmd:
		args = hcCommand[1:]
	case HealthCheckCmdShell:
		if len(hcCommand) < 2 || strings.TrimSpace(hcCommand[1]) == "" {
			return nil, fmt.Errorf("no health check command specified")
		}
		args = []string{"/bin/sh", "-c", strings.Join(hcCommand[1:], " ")}
	default:
		args = hcCommand
	}

	if len(args) < 1 || args[0] == "" {
		return nil, fmt.Errorf("no health check command specified")
	}

	// Get container spec for environment and working directory
	spec, err := container.Spec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get container spec: %w", err)
	}

	// Todo confirm if we need to merge with default PATH
	processSpec := &specs.Process{
		Args: args,
		Env:  spec.Process.Env,
		Cwd:  spec.Process.Cwd,
	}

	return processSpec, nil
}
