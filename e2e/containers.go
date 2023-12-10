package e2e

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/0xERR0R/blocky/e2e/modules/mokka"
	staticfileserver "github.com/0xERR0R/blocky/e2e/modules/staticFileServer"
	. "github.com/0xERR0R/blocky/e2e/util"
	log "github.com/sirupsen/logrus"

	"github.com/0xERR0R/blocky/config"
	"github.com/0xERR0R/blocky/helpertest"
	"github.com/0xERR0R/blocky/util"
	"github.com/avast/retry-go/v4"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/miekg/dns"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mariadb"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	redisImage        = "redis:7"
	postgresImage     = "postgres:15.2-alpine"
	mariaDBImage      = "mariadb:11"
	mokaImage         = "ghcr.io/0xerr0r/dns-mokka:0.2.0"
	staticServerImage = "halverneus/static-file-server:latest"
	blockyImage       = "blocky-e2e"

	defaultRequestTimeout = 5 * time.Second
)

func createDNSMokkaContainer(ctx context.Context, alias string, rules ...string) (*mokka.MokkaContainer, error) {
	return DeferTerminate(mokka.RunContainer(ctx,
		testcontainers.WithImage(mokaImage),
		mokka.WithMokkaRules(rules...),
		mokka.WithDNSRequestTimeout(defaultRequestTimeout),
		WithNetwork(ctx, alias),
	))
}

func createHTTPServerContainer(ctx context.Context, alias string,
	filename string, lines ...string,
) (*staticfileserver.StaticFileServerContainer, error) {
	return DeferTerminate(staticfileserver.RunContainer(ctx,
		testcontainers.WithImage(staticServerImage),
		staticfileserver.WithFile(ctx, filename, lines...),
		WithNetwork(ctx, alias),
	))
}

// startGenericContainer starts a container with the given request and attaches it to the test network
func startGenericContainer(ctx context.Context, alias string, req testcontainers.ContainerRequest,
) (testcontainers.Container, error) {
	greq := testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	}
	WithNetwork(ctx, alias).Customize(&greq)

	return testcontainers.GenericContainer(ctx, greq)
}

func createRedisContainer(ctx context.Context) (*redis.RedisContainer, error) {
	return DeferTerminate(redis.RunContainer(ctx,
		testcontainers.WithImage(redisImage),
		redis.WithLogLevel(redis.LogLevelVerbose),
		WithNetwork(ctx, "redis"),
	))
}

func createPostgresContainer(ctx context.Context) (*postgres.PostgresContainer, error) {
	const waitLogOccurrence = 2

	return DeferTerminate(postgres.RunContainer(ctx,
		testcontainers.WithImage(postgresImage),

		postgres.WithDatabase("user"),
		postgres.WithUsername("user"),
		postgres.WithPassword("user"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(waitLogOccurrence).
				WithStartupTimeout(startupTimeout)),
		WithNetwork(ctx, "postgres"),
	))
}

func createMariaDBContainer(ctx context.Context) (*mariadb.MariaDBContainer, error) {
	return DeferTerminate(mariadb.RunContainer(ctx,
		testcontainers.WithImage(mariaDBImage),
		mariadb.WithDatabase("user"),
		mariadb.WithUsername("user"),
		mariadb.WithPassword("user"),
		WithNetwork(ctx, "mariaDB"),
	))
}

const (
	modeOwner      = 700
	startupTimeout = 30 * time.Second
)

func createBlockyContainer(ctx context.Context, tmpDir *helpertest.TmpFolder,
	lines ...string,
) (testcontainers.Container, error) {
	f1 := tmpDir.CreateStringFile("config1.yaml",
		lines...,
	)

	cfg, err := config.LoadConfig(f1.Path, true)
	if err != nil {
		return nil, fmt.Errorf("can't create config struct %w", err)
	}

	req := testcontainers.ContainerRequest{
		Image: blockyImage,

		ExposedPorts: []string{"53/tcp", "53/udp", "4000/tcp"},

		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      f1.Path,
				ContainerFilePath: "/app/config.yml",
				FileMode:          modeOwner,
			},
		},
		ConfigModifier: func(c *container.Config) {
			c.Healthcheck = &container.HealthConfig{
				Interval: time.Second,
			}
		},
		WaitingFor: wait.ForHealthCheck().WithStartupTimeout(startupTimeout),
	}

	container, err := DeferTerminate(startGenericContainer(ctx, "blocky", req))
	if err != nil {
		return container, err
	}

	// check if DNS/HTTP interface is working.
	// Sometimes the internal health check returns OK, but the container port is not mapped yet
	err = checkBlockyReadiness(ctx, cfg, container)
	if err != nil {
		return container, fmt.Errorf("container not ready: %w", err)
	}

	return container, nil
}

func checkBlockyReadiness(ctx context.Context, cfg *config.Config, container testcontainers.Container) error {
	var err error

	const retryAttempts = 3

	err = retry.Do(
		func() error {
			_, err = doDNSRequest(ctx, container, util.NewMsgWithQuestion("healthcheck.blocky.", dns.Type(dns.TypeA)))

			return err
		},
		retry.OnRetry(func(n uint, err error) {
			log.Infof("Performing retry DNS request #%d: %s\n", n, err)
		}),
		retry.Attempts(retryAttempts),
		retry.DelayType(retry.BackOffDelay),
		retry.Delay(time.Second))

	if err != nil {
		return fmt.Errorf("can't perform the DNS healthcheck request: %w", err)
	}

	for _, httpPort := range cfg.Ports.HTTP {
		parts := strings.Split(httpPort, ":")
		port := parts[len(parts)-1]
		err = retry.Do(
			func() error {
				return doHTTPRequest(ctx, container, port)
			},
			retry.OnRetry(func(n uint, err error) {
				log.Infof("Performing retry HTTP request #%d: %s\n", n, err)
			}),
			retry.Attempts(retryAttempts),
			retry.DelayType(retry.BackOffDelay),
			retry.Delay(time.Second))

		if err != nil {
			return fmt.Errorf("can't perform the HTTP request: %w", err)
		}
	}

	return nil
}

func doHTTPRequest(ctx context.Context, container testcontainers.Container, containerPort string) error {
	host, port, err := getContainerHostPort(ctx, container, nat.Port(fmt.Sprintf("%s/tcp", containerPort)))
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://%s", net.JoinHostPort(host, port)), nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("received not OK status: %d", resp.StatusCode)
	}

	return err
}

func doDNSRequest(ctx context.Context, container testcontainers.Container, message *dns.Msg) (*dns.Msg, error) {
	const timeout = 5 * time.Second

	c := &dns.Client{
		Net:     "tcp",
		Timeout: timeout,
	}

	host, port, err := getContainerHostPort(ctx, container, "53/tcp")
	if err != nil {
		return nil, err
	}

	msg, _, err := c.Exchange(message, net.JoinHostPort(host, port))

	return msg, err
}

func getContainerHostPort(ctx context.Context, c testcontainers.Container, p nat.Port) (host, port string, err error) {
	res, err := c.MappedPort(ctx, p)
	if err != nil {
		return "", "", err
	}

	host, err = c.Host(ctx)

	if err != nil {
		return "", "", err
	}

	return host, res.Port(), err
}
