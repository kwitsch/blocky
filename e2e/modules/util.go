package modules

import (
	"context"
	"net"

	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
)

// GetContainerHostPort returns the host:port of a container port
func GetContainerHostPort(ctx context.Context, c testcontainers.Container, p nat.Port) (string, error) {
	port, err := c.MappedPort(ctx, p)
	if err != nil {
		return "", err
	}

	host, err := c.Host(ctx)
	if err != nil {
		return "", err
	}

	return net.JoinHostPort(host, port.Port()), err
}
