package redis

import (
	"github.com/rueian/rueidis"
)

var rdb rueidis.Client

func init() {
	client, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress:           []string{"redis:6379"},
		ClientTrackingOptions: []string{"PREFIX", "blocky:", "BCAST"},
	})
	if err != nil {
		panic(err)
	}
	rdb = client
}

func GetRedisClient() rueidis.Client {
	return rdb
}

func GetKey(k ...string) string {
	res := "blocky"
	for _, s := range k {
		res += ":" + s
	}
	return res
}
