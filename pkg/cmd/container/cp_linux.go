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
	"fmt"

	containerd "github.com/containerd/containerd/v2/client"

	"github.com/containerd/nerdctl/v2/pkg/api/types"
	"github.com/containerd/nerdctl/v2/pkg/containerutil"
	"github.com/containerd/nerdctl/v2/pkg/idutil/containerwalker"
)

// Cp copies files/folders between a running container and the local filesystem.
func Cp(ctx context.Context, client *containerd.Client, options types.ContainerCpOptions) error {
	walker := &containerwalker.ContainerWalker{
		Client: client,
		OnFound: func(ctx context.Context, found containerwalker.Found) error {
			if found.MatchCount > 1 {
				return fmt.Errorf("multiple IDs found with provided prefix: %s", found.Req)
			}
			return containerutil.CopyFiles(
				ctx,
				client,
				found.Container,
				options)
		},
	}
	count, err := walker.Walk(ctx, options.ContainerReq)

	if count == -1 {
		if err == nil {
			panic("nil error and count == -1 from ContainerWalker.Walk should never happen")
		}
		err = fmt.Errorf("unable to copy: %w", err)
	} else if count == 0 {
		if err != nil {
			err = fmt.Errorf("unable to retrieve containers with error: %w", err)
		} else {
			err = fmt.Errorf("no container found for: %s", options.ContainerReq)
		}
	}

	return err
}
