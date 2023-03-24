package stringcache

import (
	"context"
	"fmt"
	"time"

	"github.com/0xERR0R/blocky/log"
	"github.com/hako/durafmt"
	"github.com/rueian/rueidis"
)

type RedisGroupedStringCache struct {
	rdb       rueidis.Client
	groupType string
}

func NewRedisGroupedStringCache(groupType string, rdb rueidis.Client) *RedisGroupedStringCache {
	return &RedisGroupedStringCache{
		groupType: groupType,
		rdb:       rdb,
	}
}

func (r *RedisGroupedStringCache) groupKey(groupName string) string {
	return fmt.Sprintf("blocky:cache:%s:%s", r.groupType, groupName)
}

func (r *RedisGroupedStringCache) entryKey(entry string) string {
	return fmt.Sprintf("blocky:cache:%s:entries:%s", r.groupType, entry)
}

func (r *RedisGroupedStringCache) ElementCount(group string) int {
	res, err := r.rdb.DoCache(context.Background(), r.rdb.B().Scard().Key(r.groupKey(group)).Cache(), 600*time.Second).ToInt64()
	if err != nil {
		return 0
	}
	return int(res)
}

func (r *RedisGroupedStringCache) Contains(searchString string, groups []string) []string {
	start := time.Now()

	var result []string

	/* slowest
	var cmds []rueidis.CacheableTTL
	for _, group := range groups {
		cmds = append(cmds, rueidis.CT(r.rdb.B().Sismember().Key(r.cacheKey(group)).Member(searchString).Cache(), time.Second))
	}
	resps := r.rdb.DoMultiCache(context.Background(), cmds...)

	for ix, group := range groups {
		r, err := resps[ix].AsBool()
		if err != nil {
			panic(err)
		}
		if r {
			result = append(result, group)
		}
	}
	*/

	/*        faster
	for _, group := range groups {
		r, err := r.rdb.DoCache(context.Background(), r.rdb.B().Sismember().Key(r.cacheKey(group)).Member(searchString).Cache(), time.Minute).AsBool()
		if err != nil {
			panic(err)
		}
		if r {
			result = append(result, group)
			//break
		}
	}*/

	/* !!test was wrong, this errors
	var keys []string
	for _, group := range groups {
		keys = append(keys, r.groupKey(group))
	}

	result, err := r.rdb.Do(context.Background(), r.rdb.B().Fcall().Function("blocky_ismember").Numkeys(int64(len(keys))).Key(keys...).Arg(searchString).Build()).AsStrSlice()
	if err != nil {
		panic(err)
	}*/

	resp, err := r.rdb.DoCache(context.Background(), r.rdb.B().Smismember().Key(r.entryKey(searchString)).Member(groups...).Cache(), time.Hour).AsIntSlice()
	if err != nil {
		panic(err)
	}
	for ix, group := range groups {
		if resp[ix] == 1 {
			result = append(result, group)
		}
	}

	log.PrefixedLog("redis").Debugf("lookup for '%s': in groups: %v result: %v, duration %s", searchString, groups, result, durafmt.Parse(time.Since(start)).String())
	return result
}

func (r *RedisGroupedStringCache) Refresh(group string) GroupFactory {

	f := &RedisGroupFactory{
		rdb:       r.rdb,
		name:      group,
		cmds:      rueidis.Commands{},
		groupType: r.groupType,
	}

	f.cmds = append(f.cmds,
		r.rdb.B().Copy().Source(f.groupKey()).Destination(f.oldGroupKey()).Replace().Build(),
		r.rdb.B().Del().Key(f.groupKey()).Build())

	return f
}

type RedisGroupFactory struct {
	rdb       rueidis.Client
	name      string
	groupType string
	cmds      rueidis.Commands
	cnt       int
}

func (r *RedisGroupFactory) AddEntry(entry string) {
	r.cmds = append(r.cmds,
		r.rdb.B().Sadd().Key(r.groupKey()).Member(entry).Build(),
		r.rdb.B().Sadd().Key(r.entryKey(entry)).Member(r.name).Build(),
		r.rdb.B().Srem().Key(r.oldGroupKey()).Member(entry).Build())

	r.cnt++
}

func (r *RedisGroupFactory) Count() int {
	return r.cnt
}

func (r *RedisGroupFactory) Finish() {
	//set new
	_ = r.rdb.DoMulti(context.Background(), r.cmds...)

	//unset old
	resp, _ := r.rdb.Do(context.Background(), r.rdb.B().Smembers().Key(r.oldGroupKey()).Build()).AsStrSlice()
	c := rueidis.Commands{}
	for _, e := range resp {
		c = append(c, r.rdb.B().Srem().Key(r.entryKey(e)).Member(r.name).Build())
	}
	if len(c) > 0 {
		_ = r.rdb.DoMulti(context.Background(), c...)
	}
}

func (r *RedisGroupFactory) groupKey() string {
	return fmt.Sprintf("blocky:cache:%s:%s", r.groupType, r.name)
}

func (r *RedisGroupFactory) oldGroupKey() string {
	return fmt.Sprintf("blocky:cache:%s:%s-old", r.groupType, r.name)
}

func (r *RedisGroupFactory) entryKey(entry string) string {
	return fmt.Sprintf("blocky:cache:%s:entries:%s", r.groupType, entry)
}
