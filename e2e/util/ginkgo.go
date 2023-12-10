// ginkgo.go contains utility functions for ginkgo tests.
package util

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	"github.com/testcontainers/testcontainers-go"
)

const (
	stateRemoving      = "removing"
	stateNotFound      = "not found"
	stateAlreadyExists = "already exists"
)

// usedNetworks is a map of networks created by WithNetwork for cleanup purpose
//
//nolint:gochecknoglobals
var usedNetworks = make(map[string]*atomic.Int32)

// GetNetworkName returns a unique network name for the current test process.
func GetNetworkName() string {
	return fmt.Sprintf("blocky-e2e-network_%d", GinkgoParallelProcess())
}

// deferTerminate adds a defer function to terminate the container if it is running and not in removing state.
func DeferTerminate[T testcontainers.Container](container T, err error) (T, error) {
	DeferCleanup(func(ctx context.Context) error {
		if container.IsRunning() {
			// test if container is in removing state
			state, err := container.State(ctx)
			if err == nil && state.Status != stateRemoving {
				err := container.Terminate(ctx)
				// ignore error if removing is already in progress
				if err != nil && !strings.Contains(err.Error(), "already in progress") {
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		if logs := GetContainerLogs(context.Background(), container); len(logs) > 0 {
			AddReportEntry("Container logs", strings.Join(logs, "\n"))
		}
	}

	return container, err
}

// GetContainerLogs returns the logs of a container.
func GetContainerLogs(ctx context.Context, c testcontainers.Container) []string {
	if c == nil || ctx == nil || ctx.Err() != nil {
		return nil
	}

	reader, err := c.Logs(ctx)
	if err != nil {
		return nil
	}

	DeferCleanup(reader.Close)

	lines := make([]string, 0)

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if len(strings.TrimSpace(line)) > 0 {
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil
	}

	return lines
}

// WithNetwork attaches the container with the given alias to the test network
func WithNetwork(ctx context.Context, alias string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		networkName := GetNetworkName()

		network, err := testcontainers.GenericNetwork(ctx, testcontainers.GenericNetworkRequest{
			NetworkRequest: testcontainers.NetworkRequest{
				Name:           networkName,
				CheckDuplicate: true, // force the Docker provider to reuse an existing network
				Attachable:     true,
			},
		})
		if err != nil && !strings.Contains(err.Error(), stateAlreadyExists) {
			Fail(fmt.Sprintf("Failed to create network '%s'. Container won't be attached to this network: %v",
				networkName, err))

			return
		}

		// decrement the network counter when the test is finished and remove the network if it is not used anymore.
		DeferCleanup(func(ctx context.Context) {
			if counter, ok := usedNetworks[networkName]; ok {
				if counter.Load() <= 0 {
					return
				}

				counter.Add(-1)
				if counter.Load() == 0 {
					err := network.Remove(ctx)
					if err != nil &&
						!strings.Contains(err.Error(), stateNotFound) &&
						!strings.Contains(err.Error(), stateRemoving) {
						Fail(fmt.Sprintf("Failed to remove network '%s': %v", networkName, err))
					}
				}
			}
		})

		// increment the network counter
		if counter, ok := usedNetworks[networkName]; ok {
			counter.Add(1)
		} else {
			counter := atomic.Int32{}
			counter.Store(1)
			usedNetworks[networkName] = &counter
		}

		// attaching to the network because it was created with success or it already existed.
		req.Networks = append(req.Networks, networkName)

		if req.NetworkAliases == nil {
			req.NetworkAliases = make(map[string][]string)
		}

		req.NetworkAliases[networkName] = []string{alias}
	}
}
