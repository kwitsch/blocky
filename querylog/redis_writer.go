package querylog

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/0xERR0R/blocky/config"
	"github.com/0xERR0R/blocky/log"
	"github.com/0xERR0R/blocky/util"
	"github.com/go-redis/redis/v8"
	"github.com/hashicorp/go-multierror"
	"golang.org/x/net/publicsuffix"
)

const day time.Duration = time.Hour * 24

type redisLogEntry struct {
	Start         time.Time `json:"-"`
	ClientIP      string
	ClientName    string
	DurationMs    int64
	Reason        string
	ResponseType  string
	QuestionType  string
	QuestionName  string
	EffectiveTLDP string
	Answer        string
	ResponseCode  string
}

type RedisWriter struct {
	cfg            *config.QueryLogConfig
	redisCfg       *config.RedisConfig
	client         *redis.Client
	ctx            context.Context
	pendingEntries []*redisLogEntry
	lock           sync.RWMutex
	instanceName   string
	dbFlushPeriod  time.Duration
}

func NewRedisWriter(cfg config.QueryLogConfig, redisCfg *config.RedisConfig,
	dbFlushPeriod time.Duration) (*RedisWriter, error) {
	rw := RedisWriter{
		cfg:           &cfg,
		redisCfg:      redisCfg,
		ctx:           context.Background(),
		instanceName:  getInstanceName(),
		dbFlushPeriod: dbFlushPeriod,
	}

	rw.client = rw.getRedisClient()

	_, err := rw.client.Ping(rw.ctx).Result()

	if err == nil {
		go rw.periodicWrite()
	}

	return &rw, err
}

func (d *RedisWriter) Write(entry *LogEntry) {
	domain := util.ExtractDomainOnly(entry.QuestionName)
	eTLD, _ := publicsuffix.EffectiveTLDPlusOne(domain)

	e := &redisLogEntry{
		ClientIP:      entry.ClientIP,
		ClientName:    strings.Join(entry.ClientNames, "; "),
		DurationMs:    entry.DurationMs,
		Reason:        entry.ResponseReason,
		ResponseType:  entry.ResponseType,
		QuestionType:  entry.QuestionType,
		QuestionName:  domain,
		EffectiveTLDP: eTLD,
		Answer:        entry.Answer,
		ResponseCode:  entry.ResponseCode,
	}

	d.lock.Lock()
	defer d.lock.Unlock()

	d.pendingEntries = append(d.pendingEntries, e)
}

func (d *RedisWriter) CleanUp() {
	// Nothing to do
}

func (d *RedisWriter) getRetention() time.Duration {
	return time.Duration(d.cfg.LogRetentionDays * uint64(day))
}

func (d *RedisWriter) getKeyName(entry *redisLogEntry) string {
	return fmt.Sprintf("blocky:log:%s-%d", d.instanceName, entry.Start)
}

func getInstanceName() string {
	return strings.ReplaceAll(
		strings.ReplaceAll(
			strings.ReplaceAll(
				strings.ToLower(strings.TrimSpace(util.HostnameString())),
				":", ""),
			"-", ""),
		" ", "")
}

func (d *RedisWriter) getRedisClient() *redis.Client {
	db := d.redisCfg.Database

	if len(d.cfg.Target) > 0 {
		if newDb, err := strconv.Atoi(d.cfg.Target); err == nil {
			db = newDb
		}
	}

	var rdb *redis.Client
	if len(d.redisCfg.SentinelAddresses) > 0 {
		rdb = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:       d.redisCfg.Address,
			SentinelUsername: d.redisCfg.Username,
			SentinelPassword: d.redisCfg.SentinelPassword,
			SentinelAddrs:    d.redisCfg.SentinelAddresses,
			Username:         d.redisCfg.Username,
			Password:         d.redisCfg.Password,
			DB:               db,
			MaxRetries:       d.redisCfg.ConnectionAttempts,
			MaxRetryBackoff:  time.Duration(d.redisCfg.ConnectionCooldown),
		})
	} else {
		rdb = redis.NewClient(&redis.Options{
			Addr:            d.redisCfg.Address,
			Username:        d.redisCfg.Username,
			Password:        d.redisCfg.Password,
			DB:              db,
			MaxRetries:      d.redisCfg.ConnectionAttempts,
			MaxRetryBackoff: time.Duration(d.redisCfg.ConnectionCooldown),
		})
	}

	return rdb
}

func (d *RedisWriter) periodicWrite() {
	ticker := time.NewTicker(d.dbFlushPeriod)
	defer ticker.Stop()

	for {
		<-ticker.C

		err := d.doDBWrite()

		util.LogOnError("can't write entries to the database: ", err)
	}
}

func (d *RedisWriter) doDBWrite() error {
	d.lock.Lock()
	defer d.lock.Unlock()

	var err *multierror.Error

	if len(d.pendingEntries) > 0 {
		log.Log().Tracef("%d entries to write", len(d.pendingEntries))

		pipeline := d.client.Pipeline()
		defer pipeline.Close()

		threshold := 100

		for i, v := range d.pendingEntries {
			multierror.Append(err, d.addEntryToPipeline(pipeline, v))

			if i >= threshold {
				pipeline.Exec(d.ctx)

				threshold += 100
			}
		}
		pipeline.Exec(d.ctx)

		// clear the slice with pending entries
		d.pendingEntries = nil

		return err.ErrorOrNil()
	}

	return nil
}

func (d *RedisWriter) addEntryToPipeline(pipeline redis.Pipeliner, entry *redisLogEntry) error {
	binmsg, err := json.Marshal(entry)

	if err != nil {
		return err
	}

	statusCmd := pipeline.Set(d.ctx, d.getKeyName(entry), binmsg, d.getTTL())

	return statusCmd.Err()
}

func (d *RedisWriter) getTTL() time.Duration {
	return time.Duration(d.cfg.LogRetentionDays) * 24 * time.Hour
}
