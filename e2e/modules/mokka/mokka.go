package mokka

import (
	"context"
	"fmt"
	"time"

	"github.com/0xERR0R/blocky/e2e/modules"
	"github.com/miekg/dns"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	defaultImage      = "ghcr.io/0xerr0r/dns-mokka"
	requestTimeoutEnv = "DNS_REQUEST_TIMEOUT"
	tcpPort           = "53/tcp"
	udpPort           = "53/udp"
)

type MokkaContainer struct {
	testcontainers.Container
	requestTimeout time.Duration
}

// RunContainer creates an instance of the Mokka container type
func RunContainer(ctx context.Context, opts ...testcontainers.ContainerCustomizer) (*MokkaContainer, error) {
	req := testcontainers.ContainerRequest{
		Image:        defaultImage,
		ExposedPorts: []string{tcpPort, udpPort},
		WaitingFor:   wait.ForExposedPort(),
		Env:          make(map[string]string),
	}

	genericContainerReq := testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	}

	for _, opt := range opts {
		opt.Customize(&genericContainerReq)
	}

	requestTimeout := time.Second

	if strTimeout, ok := genericContainerReq.Env[requestTimeoutEnv]; ok {
		timeout, err := time.ParseDuration(strTimeout)
		if err != nil {
			requestTimeout = timeout
		}
	}

	container, err := testcontainers.GenericContainer(ctx, genericContainerReq)
	if err != nil {
		return nil, err
	}

	return &MokkaContainer{Container: container, requestTimeout: requestTimeout}, nil
}

// DNSRequest sends a DNS request to the Mokka container
func (c *MokkaContainer) DNSRequest(ctx context.Context, message *dns.Msg) (*dns.Msg, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	hostPort, err := modules.GetContainerHostPort(ctx, c, tcpPort)
	if err != nil {
		return nil, err
	}

	client := dns.Client{
		Net:     "tcp",
		Timeout: c.requestTimeout,
	}

	msg, _, err := client.Exchange(message, hostPort)

	return msg, err
}

// WithMokkaRules adds Mokka rules to the container
func WithMokkaRules(rules ...string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		for i, rule := range rules {
			req.Env[fmt.Sprintf("MOKKA_RULE_%d", i)] = rule
		}
	}
}

// WithDNSRequestTimeout sets the timeout for DNS requests
// The default is 5 seconds
func WithDNSRequestTimeout(timeout time.Duration) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		req.Env[requestTimeoutEnv] = timeout.String()
	}
}
