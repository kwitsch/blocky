package redis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/0xERR0R/blocky/config"
	"github.com/0xERR0R/blocky/util"
	"github.com/google/uuid"
	"github.com/rueian/rueidis"
	"github.com/sirupsen/logrus"
)

var c2 *Client2

func init() {
	ic2, err := New2(&config.RedisConfig{Addresses: []string{"redis:6379"}})
	if err != nil {
		panic(err)
	}
	c2 = ic2
}

func GetRedisClient() *Client2 {
	return c2
}

type Client2 struct {
	client       rueidis.Client
	maxCacheTime time.Duration
	l            *logrus.Entry
	ctx          context.Context
	id           []byte
}

// New creates a new redis client
func New2(cfg *config.RedisConfig) (*Client2, error) {
	// disable redis if no address is provided
	if cfg == nil || len(cfg.Addresses) == 0 {
		return nil, nil //nolint:nilnil
	}

	id, err := uuid.New().MarshalBinary()
	if err != nil {
		return nil, err
	}

	roption := rueidis.ClientOption{
		InitAddress:           cfg.Addresses,
		Password:              cfg.Password,
		Username:              cfg.Username,
		SelectDB:              cfg.Database,
		ClientName:            fmt.Sprintf("blocky-%s", util.HostnameString()),
		ClientTrackingOptions: []string{"PREFIX", "blocky:", "BCAST"},
	}

	if len(cfg.SentinelMasterSet) > 0 {
		roption.Sentinel = rueidis.SentinelOption{
			Username:  cfg.SentinelUsername,
			Password:  cfg.SentinelPassword,
			MasterSet: cfg.SentinelMasterSet,
		}
	}

	client, err := rueidis.NewClient(roption)
	if err != nil {
		return nil, err
	}

	res := &Client2{
		client:       client,
		maxCacheTime: time.Duration(cfg.ClientMaxCachingTime),
		id:           id,
		ctx:          context.Background(),
	}

	return res, nil
}

func (c *Client2) Get(key string) rueidis.RedisResult {
	cmd := c.client.B().Get().Key(key).Cache()
	return c.client.DoCache(c.ctx, cmd, c.maxCacheTime)
}

func (c *Client2) SetA(key string, value any, expiration time.Duration) rueidis.RedisResult {
	return c.SetS(key, fmt.Sprint(value), expiration)
}

func (c *Client2) SetB(key string, value []byte, expiration time.Duration) rueidis.RedisResult {
	return c.SetS(key, rueidis.BinaryString(value), expiration)
}

func (c *Client2) SetS(key, value string, expiration time.Duration) rueidis.RedisResult {
	cmds := make(rueidis.Commands, 0, 1)
	if expiration > 0 {
		cmds[0] = c.client.B().Setex().Key(key).Seconds(toSeconds(expiration)).Value(value).Build()
	} else {
		cmds[0] = c.client.B().Set().Key(key).Value(value).Build()
	}
	return c.client.Do(c.ctx, cmds[0])
}

func (c *Client2) Scard(key string) (int, error) {
	res, err := c.client.DoCache(c.ctx, c.client.B().Scard().Key(key).Cache(), c.maxCacheTime).ToInt64()
	if err != nil {
		return 0, err
	}
	return int(res), nil
}

func (c *Client2) Sismember(key, member string) (bool, error) {
	return c.client.DoCache(c.ctx, c.client.B().Sismember().Key(key).Member(member).Cache(), c.maxCacheTime).AsBool()
}

func (c *Client2) C() rueidis.Client {
	return c.client
}

func (c *Client2) DoMulti(cmds rueidis.Commands) []rueidis.RedisResult {
	return c.client.DoMulti(c.ctx, cmds...)
}

func (c *Client2) Close() {
	defer c.client.Close()
}

func Key(k ...string) string {
	return fmt.Sprintf("blocky:%s", strings.Join(k, ":"))
}

func NoResult(res rueidis.RedisResult) bool {
	err := res.Error()
	return err != nil && rueidis.IsRedisNil(err)
}

func toSeconds(t time.Duration) int64 {
	return int64(t.Seconds())
}
