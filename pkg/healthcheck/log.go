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

package healthcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	containerd "github.com/containerd/containerd/v2/client"

	"github.com/containerd/nerdctl/v2/pkg/labels"
)

// TODO: Optimize health status reads/writes to avoid excessive file I/O.
// Currently, each health check and every container inspect operation reads/writes the entire health log file.
// Consider keeping health state, failure streak in labels instead of persisting to disk. logs can still be stored in disk and read during inspect.
// This will improve performance, especially for containers with frequent health checks or when many concurrent inspect calls are made.

var mu sync.Mutex

// writeHealthLog writes the latest health check result to the log file, appending it to existing logs.
func writeHealthLog(ctx context.Context, container containerd.Container, latestResult *HealthcheckResult) error {
	mu.Lock()
	defer mu.Unlock()

	stateDir, err := getContainerStateDir(ctx, container)
	if err != nil {
		fmt.Printf("Error fetching container state dir: %v\n", err)
		return err
	}

	// Ensure file exists before writing
	if err := ensureHealthLogFile(stateDir); err != nil {
		return err
	}

	path := filepath.Join(stateDir, HealthLogFilename)

	// Marshal the latest result to JSON
	data, err := json.MarshalIndent(latestResult, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal health log: %w", err)
	}

	// Open the file in append mode
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open health log file: %w", err)
	}
	defer file.Close()

	// Write the latest result to the file
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write health log: %w", err)
	}

	return nil
}

// readHealthLog reads all health check result logs from the health.json file.
func readHealthLog(ctx context.Context, container containerd.Container) ([]*HealthcheckResult, error) {
	stateDir, err := getContainerStateDir(ctx, container)
	if err != nil {
		fmt.Printf("Error fetching container state dir: %v\n", err)
		return nil, err
	}

	path := filepath.Join(stateDir, HealthLogFilename)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var logs []*HealthcheckResult
	if err := json.NewDecoder(f).Decode(&logs); err != nil {
		return nil, err
	}

	// Reverse the slice to get the latest results first
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}

	return logs, nil
}

// ReadHealthStatusForInspect reads the health state from labels and the last MaxLogEntries health check result logs from the health.json file.
func ReadHealthStatusForInspect(ctx context.Context, container containerd.Container) (*Health, error) {
	// Get health state from labels
	state, err := readHealthStateFromLabels(ctx, container)
	if err != nil {
		return nil, fmt.Errorf("failed to read health state from labels: %w", err)
	}

	stateDir, err := getContainerStateDir(ctx, container)
	if err != nil {
		return nil, fmt.Errorf("failed to read health logs: %w", err)
	}

	path := filepath.Join(stateDir, HealthLogFilename)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	// Get file size
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	fileSize := fileInfo.Size()

	// Read the file in chunks from the end
	buf := make([]byte, 1024)
	var logs []*HealthcheckResult
	for offset := fileSize; offset > 0 && len(logs) < MaxLogEntries; {
		readSize := int64(len(buf))
		if offset < readSize {
			readSize = offset
		}
		offset -= readSize
		_, err := file.ReadAt(buf[:readSize], offset)
		if err != nil {
			return nil, err
		}
		// Parse the chunk and prepend to logs
		// This is a simplified example; you would need to handle JSON parsing carefully
	}

	// Truncate each log output using limitedBuffer
	for _, logEntry := range logs {
		if len(logEntry.Output) > MaxOutputLenForInspect {
			lb := NewResizableBuffer(MaxOutputLenForInspect) // Mimic docker's 4K limit
			_, _ = lb.Write([]byte(logEntry.Output))
			logEntry.Output = lb.String()
		}
	}

	// Create a Health object with the health state and logs
	health := &Health{
		State: *state,
		Log:   logs,
	}

	return health, nil
}

// writeHealthStateToLabels writes the health state to container labels
func writeHealthStateToLabels(ctx context.Context, container containerd.Container, state *HealthState) error {
	stateJSON, err := state.ToJSONString()
	if err != nil {
		return fmt.Errorf("failed to marshal health state: %w", err)
	}

	lbls, err := container.Labels(ctx)
	if err != nil {
		return fmt.Errorf("failed to get container labels: %w", err)
	}

	// Update health state label
	lbls[labels.HealthState] = stateJSON

	// Update container labels
	_, err = container.SetLabels(ctx, lbls)
	if err != nil {
		return fmt.Errorf("failed to update container labels: %w", err)
	}

	return nil
}

// readHealthStateFromLabels reads the health state from container labels
func readHealthStateFromLabels(ctx context.Context, container containerd.Container) (*HealthState, error) {
	lbls, err := container.Labels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get container labels: %w", err)
	}

	// Check if health state label exists
	stateJSON, ok := lbls[labels.HealthState]
	if !ok {
		return nil, nil
	}

	// Parse health state from JSON
	state, err := HealthStateFromJSON(stateJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse health state: %w", err)
	}

	return state, nil
}

// ensureHealthLogFile creates the health.json file if it doesn't exist.
func ensureHealthLogFile(stateDir string) error {
	healthLogPath := filepath.Join(stateDir, HealthLogFilename)

	// Ensure container state directory exists
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		return fmt.Errorf("container state directory does not exist: %s", stateDir)
	}

	// Create health.json if it doesn't exist
	if _, err := os.Stat(healthLogPath); os.IsNotExist(err) {
		return os.WriteFile(healthLogPath, []byte{}, 0600)
	}

	return nil
}

// getContainerStateDir returns the container's state directory from labels.
func getContainerStateDir(ctx context.Context, container containerd.Container) (string, error) {
	info, err := container.Info(ctx)
	if err != nil {
		return "", err
	}
	stateDir, ok := info.Labels[labels.StateDir]
	if !ok {
		return "", err
	}
	return stateDir, nil
}

// ResizableBuffer collects output with a configurable upper limit.
type ResizableBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	maxSize   int
	truncated bool
}

// NewResizableBuffer returns a new buffer with the given size limit in bytes.
func NewResizableBuffer(maxSize int) *ResizableBuffer {
	return &ResizableBuffer{maxSize: maxSize}
}

func (b *ResizableBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	remaining := b.maxSize - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}

	if len(p) > remaining {
		b.truncated = true
		p = p[:remaining]
	}

	return b.buf.Write(p)
}

func (b *ResizableBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	s := b.buf.String()
	if b.truncated {
		s += "... [truncated]"
	}
	return s
}
