package expirationcache

import (
	"fmt"
	"strconv"
	"time"

	"github.com/0xERR0R/blocky/redis"
	"github.com/miekg/dns"
)

type RedisCache struct {
	rdb  *redis.Client2
	name string
}

func (r *RedisCache) Put(key string, val interface{}, expiration time.Duration) {
	switch v := val.(type) {
	case dns.Msg:
		b, err := v.Pack()
		if err != nil {
			panic(err)
		}
		err = r.rdb.SetB(r.cacheKey(key), b, expiration).Error()
		if err != nil {
			panic(err)
		}
	case int:
		err := r.rdb.SetA(r.cacheKey(key), v, expiration).Error()
		if err != nil {
			panic(err)
		}
	default:
		fmt.Println("type unknown")
	}

}

func (r *RedisCache) Get(key string) (val interface{}, expiration time.Duration) {
	resp := r.rdb.Get(r.cacheKey(key))
	if redis.NoResult(resp) {
		return nil, 0
	}
	err := resp.Error()
	if err != nil {
		panic(err)
	}

	respStr, err := resp.ToString()
	if err != nil {
		panic(err)
	}
	bytesVal := []byte(respStr)

	if len(bytesVal) <= 2 {
		code, err := strconv.Atoi(string(bytesVal))
		if err != nil {
			panic(err)
		}
		return code, time.Duration(resp.CacheTTL() * int64(time.Second))
	}

	msg := new(dns.Msg)
	err = msg.Unpack(bytesVal)
	if err != nil {
		panic(err)
	}

	return msg, time.Duration(resp.CacheTTL() * int64(time.Second))
}

func (r *RedisCache) TotalCount() int {
	//TODO implement me
	return 0
}

func (r *RedisCache) Clear() {
	//TODO implement me

}

func NewRedisCache(rdb *redis.Client2, name string) *RedisCache {

	return &RedisCache{
		rdb:  rdb,
		name: name,
	}
}

func (r *RedisCache) cacheKey(key string) string {
	return redis.Key("cache", r.name, key)
}
