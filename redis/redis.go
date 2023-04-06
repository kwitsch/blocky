package redis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/0xERR0R/blocky/config"
	"github.com/0xERR0R/blocky/log"
	"github.com/0xERR0R/blocky/model"
	"github.com/0xERR0R/blocky/util"
	"github.com/google/uuid"
	"github.com/miekg/dns"
	"github.com/rueian/rueidis"
	"github.com/rueian/rueidis/rueidislock"
	"github.com/sirupsen/logrus"
)

const (
	SyncChannelName         = "blocky_sync"
	chanCap                 = 1000
	cacheReason             = "EXTERNAL_CACHE"
	defaultCacheTime        = 1 * time.Minute
	workerNeogationInterval = 5 * time.Second
	messageTypeCache        = 0
	messageTypeEnable       = 1
)

// redis pubsub message
type redisMessage struct {
	Key     string `json:"k,omitempty"`
	Type    int    `json:"t"`
	Message []byte `json:"m"`
	Client  []byte `json:"c"`
}

// Client for redis communication
type Client struct {
	config         *config.RedisConfig
	client         rueidis.Client
	locker         rueidislock.Locker
	mux            sync.RWMutex
	l              *logrus.Entry
	ctx            context.Context
	ctxCancel      context.CancelFunc
	id             []byte
	sendBuffer     chan *responseBufferMessage
	isWorker       bool
	CacheChannel   chan *CacheMessage
	EnabledChannel chan *EnabledMessage
}

// New creates a new redis client
func New(cfg *config.RedisConfig) (*Client, error) {
	// disable redis if no address is provided
	if cfg == nil || len(cfg.Addresses) == 0 {
		return nil, nil //nolint:nilnil
	}

	id, err := uuid.New().MarshalBinary()
	if err != nil {
		return nil, err
	}

	l := log.PrefixedLog("redis")

	client, err := getRueidisClient(cfg, l)
	if err != nil {
		return nil, err
	}

	locker, err := getRueidisLocker(cfg, l)
	if err != nil {
		return nil, err
	}

	ctx, cancelFn := context.WithCancel(context.Background())

	res := &Client{
		config:         cfg,
		client:         client,
		locker:         locker,
		l:              l,
		ctx:            ctx,
		ctxCancel:      cancelFn,
		id:             id,
		sendBuffer:     make(chan *responseBufferMessage, chanCap),
		CacheChannel:   make(chan *CacheMessage, chanCap),
		EnabledChannel: make(chan *EnabledMessage, chanCap),
	}

	go res.startup()

	return res, nil
}

// PublishCache publish cache to redis async
func (c *Client) PublishCache(key string, message *dns.Msg) {
	if len(key) > 0 && message != nil {
		c.sendBuffer <- &responseBufferMessage{
			Key:     key,
			Message: message,
		}
	}
}

func (c *Client) PublishEnabled(state *EnabledMessage) {
	binState, sErr := json.Marshal(state)
	if sErr == nil {
		binMsg, mErr := json.Marshal(redisMessage{
			Type:    messageTypeEnable,
			Message: binState,
			Client:  c.id,
		})

		if mErr == nil {
			c.client.Do(c.ctx,
				c.client.B().Publish().
					Channel(SyncChannelName).
					Message(rueidis.BinaryString(binMsg)).
					Build())
		}
	}
}

// GetRedisCache reads the redis cache and publish it to the channel
func (c *Client) GetRedisCache() {
	c.l.Debug("GetRedisCache")

	go func() {
		var cursor uint64

		searchKey := CategoryKey(KeyCategoryCache, "*")

		for {
			sres, err := c.client.Do(c.ctx, c.client.B().Scan().Cursor(cursor).Match(searchKey).Count(1).Build()).AsScanEntry()
			if err != nil {
				c.l.Error("GetRedisCache ", err)

				break
			}

			cursor = sres.Cursor

			for _, key := range sres.Elements {
				c.l.Debugf("GetRedisCache: %s", key)

				resp := c.client.Do(c.ctx, c.client.B().Get().Key(key).Build())

				if resp.Error() == nil {
					str, err := resp.ToString()
					if err == nil {
						response, err := c.getResponse(str)
						if err == nil {
							c.CacheChannel <- response
						}
					}
				}
			}

			if cursor == 0 { // no more keys
				break
			}
		}
	}()
}

func (c *Client) Close() {
	defer c.ctxCancel()
	defer c.client.Close()
	defer c.locker.Close()
	defer close(c.sendBuffer)
	defer close(c.CacheChannel)
	defer close(c.EnabledChannel)
}

func (c *Client) IsWorker() bool {
	c.mux.RLock()
	defer c.mux.RUnlock()
	return c.isWorker
}

func (c *Client) DoWorkerTask(name string, task func(context.Context) error) error {
	if !c.IsWorker() {
		return nil
	}

	err := c.RunLocked(LockTypeWorkerTask, name, false, task)
	if err == rueidislock.ErrNotLocked {
		return nil
	}

	return err
}

func (c *Client) RunLocked(lt LockType, suf string, wait bool, task func(context.Context) error) error {
	key := lt.String()
	if len(suf) > 0 {
		key = fmt.Sprintf("%s-%s", key, suf)
	}

	var ctx context.Context
	var cancel context.CancelFunc
	var err error

	if wait {
		ctx, cancel, err = c.locker.WithContext(context.Background(), key)
	} else {
		ctx, cancel, err = c.locker.TryWithContext(context.Background(), key)
	}

	if err != nil {
		return err
	}

	defer cancel()

	return task(ctx)
}

func getRueidisClient(cfg *config.RedisConfig, l *logrus.Entry) (rueidis.Client, error) {
	coption := generateClientOptions(cfg, false)

	var err error

	for i := 0; i < cfg.ConnectionAttempts; i++ {
		l.Debugf("client connection attempt %d", i)

		client, err := rueidis.NewClient(coption)

		if err == nil {
			l.Debug("client connected")
			return client, nil
		} else {
			l.Debug(err)
		}

		time.Sleep(time.Duration(cfg.ConnectionCooldown))
	}

	l.Debugf("client connection error: %v", err)

	return nil, err
}

func getRueidisLocker(cfg *config.RedisConfig, l *logrus.Entry) (rueidislock.Locker, error) {
	coption := generateClientOptions(cfg, true)
	loption := rueidislock.LockerOption{
		ClientOption: coption,
		KeyMajority:  3,
		KeyPrefix:    Key(KeyCategoryLock.String()),
	}

	var err error

	for i := 0; i < cfg.ConnectionAttempts; i++ {
		l.Debugf("locker connection attempt %d", i)

		locker, err := rueidislock.NewLocker(loption)

		if err == nil {
			l.Debug("locker connected")
			return locker, nil
		} else {
			l.Debug(err)
		}

		time.Sleep(time.Duration(cfg.ConnectionCooldown))
	}

	l.Debugf("locker connection error: %v", err)

	return nil, err
}

func generateClientOptions(cfg *config.RedisConfig, locker bool) rueidis.ClientOption {
	res := rueidis.ClientOption{
		InitAddress:       cfg.Addresses,
		Password:          cfg.Password,
		Username:          cfg.Username,
		SelectDB:          cfg.Database,
		RingScaleEachConn: cfg.ConnRingScale,
		CacheSizeEachConn: cfg.LocalCacheSize,
	}

	if locker {
		res.ClientName = fmt.Sprintf("blocky_locker-%s", util.HostnameString())
	} else {
		res.ClientName = fmt.Sprintf("blocky-%s", util.HostnameString())
		res.ClientTrackingOptions = []string{"PREFIX", "blocky:", "BCAST"}
	}

	if len(cfg.SentinelMasterSet) > 0 {
		res.Sentinel = rueidis.SentinelOption{
			Username:  cfg.SentinelUsername,
			Password:  cfg.SentinelPassword,
			MasterSet: cfg.SentinelMasterSet,
		}
	}

	return res
}

// startup starts a new goroutine for subscription and translation
func (c *Client) startup() {
	disconnect, closePSClient := c.setupPubSub()
	defer closePSClient()

	timer := time.NewTicker(workerNeogationInterval)
	defer timer.Stop()

	for {
		select {
		// subscriber disconnected -> reconnect
		case <-disconnect:
			closePSClient()
			disconnect, closePSClient = c.setupPubSub()
		case <-timer.C:
			c.neogateWorker()
		// redis client is closed
		case <-c.ctx.Done():
			return
		// publish message from buffer
		case s := <-c.sendBuffer:
			c.publishMessageFromBuffer(s)
		}
	}
}

func (c *Client) neogateWorker() {
	key := CategoryKey(KeyCategoryConfig, "active_worker_weight")
	oww := int64(c.config.WorkerWeight)
	wnis := int64(workerNeogationInterval.Seconds())

	c.RunLocked(LockTypeWorkerNeogation, "", true, func(ctx context.Context) error {
		caw, err := c.client.DoCache(ctx, c.client.B().Get().Key(key).Cache(), workerNeogationInterval).AsInt64()
		if rueidis.IsRedisNil(err) || (err == nil && caw < oww) {
			c.client.DoMulti(ctx,
				c.client.B().Set().Key(key).Value(fmt.Sprintf("%d", oww)).Build(),
				c.client.B().Expire().Key(key).Seconds(wnis).Build())
			return nil
		} else if err != nil {
			return err
		}

		if caw == oww {
			return c.client.Do(ctx, c.client.B().Expire().Key(key).Seconds(wnis).Build()).Error()
		}

		return nil
	})

	c.mux.Lock()
	defer c.mux.Unlock()
	caw, err := c.client.DoCache(c.ctx, c.client.B().Get().Key(key).Cache(), workerNeogationInterval).AsInt64()
	if err == nil {
		c.isWorker = (caw == oww)
	}

}

func (c *Client) setupPubSub() (<-chan error, func()) {
	dc, cancel := c.client.Dedicate()

	disconnect := dc.SetPubSubHooks(rueidis.PubSubHooks{
		OnMessage: func(m rueidis.PubSubMessage) {
			if m.Channel == SyncChannelName {
				c.l.Debug("Received message: ", m)

				if len(m.Message) > 0 {
					// message is not empty
					c.processReceivedMessage(m.Message)
				}
			}
		},
	})

	dc.Do(c.ctx, dc.B().Subscribe().Channel(SyncChannelName).Build())

	return disconnect, cancel
}

func (c *Client) publishMessageFromBuffer(s *responseBufferMessage) {
	origRes := s.Message
	origRes.Compress = true
	binRes, pErr := origRes.Pack()

	if pErr == nil {
		binMsg, mErr := json.Marshal(redisMessage{
			Key:     s.Key,
			Type:    messageTypeCache,
			Message: binRes,
			Client:  c.id,
		})

		if mErr == nil {
			c.client.Do(c.ctx,
				c.client.B().Publish().
					Channel(SyncChannelName).
					Message(rueidis.BinaryString(binMsg)).
					Build())
		}

		c.client.Do(c.ctx,
			c.client.B().Setex().
				Key(CategoryKey(KeyCategoryCache, "query", "response", s.Key)).
				Seconds(int64(c.getTTL(origRes).Seconds())).
				Value(rueidis.BinaryString(binRes)).
				Build())
	}
}

func (c *Client) processReceivedMessage(msgPayload string) {
	var rm redisMessage

	err := json.Unmarshal([]byte(msgPayload), &rm)
	if err == nil {
		// message was sent from a different blocky instance
		if !bytes.Equal(rm.Client, c.id) {
			switch rm.Type {
			case messageTypeCache:
				var cm *CacheMessage

				cm, err = convertMessage(&rm, 0)
				if err == nil {
					c.CacheChannel <- cm
				}
			case messageTypeEnable:
				err = c.processEnabledMessage(&rm)
			default:
				c.l.Warn("Unknown message type: ", rm.Type)
			}
		}
	}

	if err != nil {
		c.l.Error("Processing error: ", err)
	}
}

func (c *Client) processEnabledMessage(redisMsg *redisMessage) error {
	var msg EnabledMessage

	err := json.Unmarshal(redisMsg.Message, &msg)
	if err == nil {
		c.EnabledChannel <- &msg
	}

	return err
}

// getResponse returns model.Response for a key
func (c *Client) getResponse(key string) (*CacheMessage, error) {
	resp, err := c.client.Do(c.ctx, c.client.B().Get().Key(key).Build()).AsBytes()
	if err == nil {
		uittl, err := c.client.Do(c.ctx,
			c.client.B().Ttl().
				Key(key).
				Build()).AsUint64()
		ttl := time.Second * time.Duration(uittl)

		if err == nil {
			var result *CacheMessage

			result, err = convertMessage(&redisMessage{
				Key:     strings.TrimPrefix(key, CategoryKey(KeyCategoryCache)),
				Message: resp,
			}, ttl)
			if err != nil {
				return nil, fmt.Errorf("conversion error: %w", err)
			}

			return result, nil
		}
	}

	return nil, err
}

// convertMessage converts redisMessage to CacheMessage
func convertMessage(message *redisMessage, ttl time.Duration) (*CacheMessage, error) {
	msg := dns.Msg{}

	err := msg.Unpack(message.Message)
	if err == nil {
		if ttl > 0 {
			for _, a := range msg.Answer {
				a.Header().Ttl = uint32(ttl.Seconds())
			}
		}

		res := &CacheMessage{
			Key: message.Key,
			Response: &model.Response{
				RType:  model.ResponseTypeCACHED,
				Reason: cacheReason,
				Res:    &msg,
			},
		}

		return res, nil
	}

	return nil, err
}

// getTTL of dns message or return defaultCacheTime if 0
func (c *Client) getTTL(dns *dns.Msg) time.Duration {
	ttl := uint32(0)
	for _, a := range dns.Answer {
		if a.Header().Ttl > ttl {
			ttl = a.Header().Ttl
		}
	}

	if ttl == 0 {
		return defaultCacheTime
	}

	return time.Duration(ttl) * time.Second
}

func Key(sks ...string) string {
	return fmt.Sprintf("blocky:%s", strings.Join(sks, ":"))
}

func CategoryKey(kc KeyCategory, sks ...string) string {
	return fmt.Sprintf("blocky:%s:%s", kc.String(), strings.Join(sks, ":"))
}
