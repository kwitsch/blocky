package staticfileserver

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"github.com/0xERR0R/blocky/e2e/modules"
	"github.com/testcontainers/testcontainers-go"
)

const (
	defaultImage = "halverneus/static-file-server"
	httpPort     = "8080/tcp"
	filePrefix   = "sfs_container"
	modeOwner    = 700
)

type StaticFileServerContainer struct {
	testcontainers.Container
}

// RunContainer creates an instance of the StaticFileServer container type
func RunContainer(ctx context.Context, opts ...testcontainers.ContainerCustomizer) (*StaticFileServerContainer, error) {
	req := testcontainers.ContainerRequest{
		Image:        defaultImage,
		ExposedPorts: []string{httpPort},
		Env:          map[string]string{"FOLDER": "/"},
		Files:        make([]testcontainers.ContainerFile, 0),
	}

	req.LifecycleHooks = []testcontainers.ContainerLifecycleHooks{
		{
			PostTerminates: []testcontainers.ContainerHook{
				func(ctx context.Context, c testcontainers.Container) error {
					for _, f := range req.Files {
						if err := os.Remove(f.HostFilePath); err != nil {
							return err
						}
					}

					return nil
				},
			},
		},
	}

	genericContainerReq := testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	}

	for _, opt := range opts {
		opt.Customize(&genericContainerReq)
	}

	container, err := testcontainers.GenericContainer(ctx, genericContainerReq)
	if err != nil {
		return nil, err
	}

	return &StaticFileServerContainer{Container: container}, nil
}

// GetUrl returns the URL of the container
func (c *StaticFileServerContainer) GetURL(ctx context.Context) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	hostPort, err := modules.GetContainerHostPort(ctx, c, httpPort)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("http://%s", hostPort), nil
}

// GetFileURL returns the URL of the container with the given filename
func (c *StaticFileServerContainer) GetFileURL(ctx context.Context, filename string) (string, error) {
	base, err := c.GetURL(ctx)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s/%s", base, filename), nil
}

// WithFile adds a file with the given lines to the container
func WithFile(ctx context.Context, filename string, lines ...string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		hostFile, err := writeStringFile(lines...)
		if err != nil {
			panic(err)
		}

		req.Files = append(req.Files, testcontainers.ContainerFile{
			HostFilePath:      hostFile,
			ContainerFilePath: filename,
			FileMode:          modeOwner,
		})
	}
}

// writeStringFile writes the given lines to a temporary file and returns the file name
func writeStringFile(lines ...string) (string, error) {
	file, err := os.CreateTemp("", filePrefix)
	if err != nil {
		return "", err
	}
	defer file.Close()

	first := true
	w := bufio.NewWriter(file)

	for _, l := range lines {
		if first {
			first = false
		} else {
			if _, err := w.WriteString("\n"); err != nil {
				return "", err
			}
		}

		if _, err := w.WriteString(l); err != nil {
			return "", err
		}
	}

	return file.Name(), w.Flush()
}
