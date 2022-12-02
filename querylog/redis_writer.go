package querylog

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/0xERR0R/blocky/config"
	"github.com/0xERR0R/blocky/redis"
	"github.com/0xERR0R/blocky/util"
	"golang.org/x/net/publicsuffix"
)

const day time.Duration = time.Hour * 24

type redisLogEntry struct {
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
	redisClient *redis.Client
	cfg         *config.QueryLogConfig
}

func NewRedisWriter(cfg config.QueryLogConfig, redisClient *redis.Client) *RedisWriter {
	return &RedisWriter{
		cfg:         &cfg,
		redisClient: redisClient,
	}
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

	if binmsg, err := json.Marshal(e); err == nil {
		k := getKeyName(entry)

		d.redisClient.Set(k, binmsg, d.getRetention())
	}
}

func (d *RedisWriter) CleanUp() {
	// Nothing to do
}

func (d *RedisWriter) getRetention() time.Duration {
	return time.Duration(d.cfg.LogRetentionDays * uint64(day))
}

func getKeyName(entry *LogEntry) string {
	return fmt.Sprintf("blocky:log:%d", entry.Start)
}
