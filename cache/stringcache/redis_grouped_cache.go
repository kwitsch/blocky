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

func (r *RedisGroupedStringCache) cacheKey(groupName string) string {
	return fmt.Sprintf("blocky:cache:%s:%s", r.groupType, groupName)
}

func (r *RedisGroupedStringCache) ElementCount(group string) int {
	res, err := r.rdb.DoCache(context.Background(), r.rdb.B().Scard().Key(r.cacheKey(group)).Cache(), 600*time.Second).ToInt64()
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

	/* fastest */
	var keys []string
	for _, group := range groups {
		keys = append(keys, r.cacheKey(group))
	}

	r.rdb.B().Fcall().Function("blocky_ismember").Numkeys(int64(len(keys))).Key(keys...).Arg(searchString).Build()

	log.PrefixedLog("redis").Debugf("lookup for '%s': in groups: %v result: %v, duration %s", searchString, groups, result, durafmt.Parse(time.Since(start)).String())
	return result
}

func (r *RedisGroupedStringCache) Refresh(group string) GroupFactory {
	cmds := rueidis.Commands{r.rdb.B().Del().Key(r.cacheKey(group)).Build()}

	f := &RedisGroupFactory{
		rdb:  r.rdb,
		name: r.cacheKey(group),
		cmds: cmds,
	}

	return f
}

type RedisGroupFactory struct {
	rdb  rueidis.Client
	name string
	cmds rueidis.Commands
	cnt  int
}

func (r *RedisGroupFactory) AddEntry(entry string) {
	r.cmds = append(r.cmds, r.rdb.B().Sadd().Key(r.name).Member(entry).Build())

	r.cnt++
}

func (r *RedisGroupFactory) Count() int {
	return r.cnt
}

func (r *RedisGroupFactory) Finish() {
	_ = r.rdb.DoMulti(context.Background(), r.cmds...)
}
