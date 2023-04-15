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
// timer // redis timer
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
