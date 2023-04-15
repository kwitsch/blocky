package redis

import "time"

type Ticker struct {
	name string

	C <-chan time.Time
}
