package expirationcache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/miekg/dns"
	"github.com/rueian/rueidis"
)

type RedisCache struct {
	ctx  context.Context
	rdb  rueidis.Client
	name string
}

func (r *RedisCache) Put(key string, val interface{}, expiration time.Duration) {
	switch v := val.(type) {
	case dns.Msg:
		b, err := v.Pack()
		if err != nil {
			panic(err)
		}
		err = r.rdb.Do(r.ctx, r.rdb.B().Setex().
			Key(r.name+":"+key).Seconds(int64(expiration.Seconds())).
			Value(rueidis.BinaryString(b)).Build()).Error()
		if err != nil {
			panic(err)
		}
	case int:
		err := r.rdb.Do(r.ctx, r.rdb.B().Setex().
			Key(r.name+":"+key).Seconds(int64(expiration.Seconds())).
			Value(fmt.Sprint(v)).Build()).Error()
		if err != nil {
			panic(err)
		}
	default:
		fmt.Println("type unknown")
	}

}

func (r *RedisCache) Get(key string) (val interface{}, expiration time.Duration) {
	resp := r.rdb.DoCache(r.ctx, r.rdb.B().Get().Key(r.name+":"+key).Cache(), time.Hour)
	if resp.Error() != nil {
		panic(resp.Error())
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

func NewRedisCache(rdb rueidis.Client, name string) *RedisCache {

	return &RedisCache{
		ctx:  context.Background(),
		rdb:  rdb,
		name: name,
	}
}
