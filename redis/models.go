package redis

import (
	"time"

	"github.com/0xERR0R/blocky/model"
	"github.com/miekg/dns"
)

//go:generate go run github.com/abice/go-enum -f=$GOFILE --marshal --names

// KeyCategory represents the top category of a redis key(blocky:*KeyCategory*:*key*) ENUM(
// cache // cache entries
// config // config entries
// lock // locks
// log // logs
// )
type KeyCategory string

// ChannelName represents syncronisation channel name ENUM(
// blocky_worker_neogation // channel to determin which block instance should act as worker
// )
type ChannelName string

// LockType represents used redis locks ENUM(
// worker_neogation // neogate currently used workers
// )
type LockType string

// Redis CacheChannel message
type CacheMessage struct {
	Key      string
	Response *model.Response
}

// sendBuffer message
type responseBufferMessage struct {
	Key     string
	Message *dns.Msg
}

// Redis EnabledChannel message
type EnabledMessage struct {
	State    bool          `json:"s"`
	Duration time.Duration `json:"d,omitempty"`
	Groups   []string      `json:"g,omitempty"`
}
