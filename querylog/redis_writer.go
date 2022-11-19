package querylog

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/0xERR0R/blocky/config"
	"github.com/0xERR0R/blocky/redis"
	"github.com/0xERR0R/blocky/util"
	"github.com/miekg/dns"
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
	domain := util.ExtractDomain(entry.Request.Req.Question[0])
	eTLD, _ := publicsuffix.EffectiveTLDPlusOne(domain)

	e := &redisLogEntry{
		ClientIP:      entry.Request.ClientIP.String(),
		ClientName:    strings.Join(entry.Request.ClientNames, "; "),
		DurationMs:    entry.DurationMs,
		Reason:        entry.Response.Reason,
		ResponseType:  entry.Response.RType.String(),
		QuestionType:  dns.TypeToString[entry.Request.Req.Question[0].Qtype],
		QuestionName:  domain,
		EffectiveTLDP: eTLD,
		Answer:        util.AnswerToString(entry.Response.Res.Answer),
		ResponseCode:  dns.RcodeToString[entry.Response.Res.Rcode],
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
