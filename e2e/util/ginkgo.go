// ginkgo.go contains utility functions for ginkgo tests.
package util

import (
	"bufio"
	"context"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/testcontainers/testcontainers-go"
)

const (
	containerStateRemoving = "removing"
)

// deferTerminate adds a defer function to terminate the container if it is running and not in removing state.
func DeferTerminate[T testcontainers.Container](container T, err error) (T, error) {
	ginkgo.DeferCleanup(func(ctx context.Context) error {
		if container.IsRunning() {
			state, err := container.State(ctx)
			if err == nil && state.Status != containerStateRemoving {
				return container.Terminate(ctx)
			}
		}

		return nil
	})

	return container, err
}

// GetContainerLogs returns the logs of a container.
func GetContainerLogs(ctx context.Context, c testcontainers.Container) []string {
	if c == nil || ctx == nil || ctx.Err() != nil {
		return nil
	}

	reader, err := c.Logs(ctx)

	ginkgo.DeferCleanup(func() {
		reader.Close()
	})
	gomega.Expect(err).Should(gomega.Succeed())

	if err != nil {
		return nil
	}

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
