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
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/nerdctl/v2/pkg/api/types"
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
	Status        string    `json:"status"`        // Status is the current health status
	FailingStreak int       `json:"failingStreak"` // FailingStreak is the number of consecutive failures
	Log           []Log     `json:"log"`           // Log contains the last few health check logs
	Start         time.Time `json:"start"`         // Start is when the health check started
	End           time.Time `json:"end"`           // End is when the health check ended
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
	// verify container status
	if err := verifyContainerStatus(container); err != nil {
		return err
	}

	// Get container info to access labels
	info, err := container.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get container info: %w", err)
	}

	// Check if container has health check configured
	configJSON, ok := info.Labels["healthcheck/config"]
	if !ok {
		return fmt.Errorf("container has no health check configured")
	}

	// Parse health check configuration from labels
	var config HealthConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return fmt.Errorf("invalid health check configuration: %w", err)
	}

	// Verify health check command exists
	if len(config.Test) == 0 {
		return fmt.Errorf("health check command is empty")
	}

	// Get container task
	task, err := container.Task(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to get container task: %w", err)
	}

	// Create process spec for health check command
	processSpec := &specs.Process{
		Args: config.Test,
		Env: []string{
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		Cwd: "/",
	}

	// Create context with timeout for health check
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(config.Timeout))
	defer cancel()

	// Execute health check command with proper IO handling
	execOpts := cio.NewCreator(
		cio.WithStdio,
		cio.WithTerminal,
	)

	// Generate a unique exec ID using timestamp and random number
	rand.Seed(time.Now().UnixNano())
	execID := fmt.Sprintf("health-check-%d-%d", time.Now().UnixNano(), rand.Int63())
	process, err := task.Exec(execCtx, execID, processSpec, execOpts)
	if err != nil {
		return fmt.Errorf("failed to execute health check: %w", err)
	}

	// Wait for process to complete
	exitStatusC, err := process.Wait(execCtx)
	if err != nil {
		return fmt.Errorf("failed to wait for health check: %w", err)
	}
	exitStatus := <-exitStatusC
	code, _, err := exitStatus.Result()
	if err != nil {
		return fmt.Errorf("failed to wait for health check: %w", err)
	}

	// Update health status based on exit code
	status := "healthy"
	if code != 0 {
		status = "unhealthy"
	}

	// Update health status in container labels
	statusJSON := fmt.Sprintf(`{"Status":"%s","FailingStreak":0}`, status)
	_, err = container.SetLabels(ctx, map[string]string{
		"healthcheck/status": statusJSON,
	})
	if err != nil {
		return fmt.Errorf("failed to update health status: %w", err)
	}

	return nil
}

func verifyContainerStatus(container containerd.Container) error {
	// Get container task to check status
	task, err := container.Task(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("failed to get container task: %w", err)
	}

	// Check if container is running
	status, err := task.Status(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get container status: %w", err)
	}
	if status.Status != containerd.Running {
		return fmt.Errorf("container is not running (status: %s)", status.Status)
	}

	return nil
}

// updateHealthStatus updates the health status based on the health check result
func updateHealthStatus(healthStatus *HealthStatus, healthConfig *HealthConfig, exitCode uint32, output string, startTime, endTime time.Time) {
	// Create log entry
	log := Log{
		Start:    startTime,
		End:      endTime,
		ExitCode: int(exitCode),
		Output:   output,
	}

	// Update health status based on exit code
	if exitCode == 0 {
		// Reset failing streak on success
		healthStatus.FailingStreak = 0
		healthStatus.Status = Healthy
	} else {
		// Increment failing streak
		healthStatus.FailingStreak++
		if healthStatus.FailingStreak >= healthConfig.Retries {
			healthStatus.Status = Unhealthy
			healthStatus.End = endTime
		}
	}

	// Keep only the last 5 logs
	healthStatus.Log = append(healthStatus.Log, log)
	if len(healthStatus.Log) > 5 {
		healthStatus.Log = healthStatus.Log[len(healthStatus.Log)-5:]
	}
}

// prepareProcessSpec prepares the process spec for health check execution
func prepareProcessSpec(healthConfig *HealthConfig) (*specs.Process, error) {
	if len(healthConfig.Test) == 0 {
		return nil, fmt.Errorf("no health check command specified")
	}

	cmdType := healthConfig.Test[0]
	var args []string

	switch cmdType {
	case HealthCheckCmdNone, HealthCheckTestNone:
		return nil, fmt.Errorf("no health check defined")
	case HealthCheckCmd:
		args = healthConfig.Test[1:]
	case HealthCheckCmdShell:
		args = []string{"/bin/sh", "-c", strings.Join(healthConfig.Test[1:], " ")}
	default:
		// If no command type specified, use the command as is
		args = healthConfig.Test
	}

	processSpec := &specs.Process{
		Args: args,
		Env: []string{
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		Cwd: "/",
	}

	return processSpec, nil
}
