//go:generate go run github.com/abice/go-enum -f=$GOFILE --marshal --names

package redis

import (
	"time"

	"github.com/0xERR0R/blocky/model"
	"github.com/miekg/dns"
)

// KeyCategory represents the top category of a redis key(blocky:*KeyCategory*:*key*) ENUM(
// cache // cache entries
// config // config entries
// ticker // ticker
// lock // locks
// log // logs
// )
type KeyCategory string

// sendBuffer message
type bufferMessage struct {
	Key     string
	Message *dns.Msg
}

// redis pubsub message
type redisMessage struct {
	Key     string `json:"k,omitempty"`
	Type    int    `json:"t"`
	Message []byte `json:"m"`
	Client  []byte `json:"c"`
}

// CacheChannel message
type CacheMessage struct {
	Key      string
	Response *model.Response
}

type EnabledMessage struct {
	State    bool          `json:"s"`
	Duration time.Duration `json:"d,omitempty"`
	Groups   []string      `json:"g,omitempty"`
}

type TTL time.Duration

func (ttl TTL) Duration() time.Duration {
	return time.Duration(ttl)
}

func (ttl TTL) Redis() int64 {
	return int64(ttl.Duration().Seconds())
}

func (ttl TTL) DNS() uint32 {
	return uint32(ttl.Duration().Seconds())
}

func TtlFromDnsMsg(msg *dns.Msg, defTtl TTL) TTL {
	ittl := uint32(0)
	for _, a := range msg.Answer {
		if a.Header().Ttl > ittl {
			ittl = a.Header().Ttl
		}
	}

	if ittl == 0 && defTtl > 0 {
		return defTtl
	}

	return TTL(time.Duration(ittl) * time.Second)
}

func TtlFromSeconds(s uint64) TTL {
	return TTL(time.Duration(s) * time.Second)
}
