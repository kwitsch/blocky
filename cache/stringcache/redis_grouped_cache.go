package stringcache

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/0xERR0R/blocky/log"
	"github.com/hako/durafmt"
	"github.com/rueian/rueidis"
)

const cacheGrp string = "__cache__"

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

func (r *RedisGroupedStringCache) entryKey(searchString string, groups []string) string {
	return fmt.Sprintf("%s-%s",
		strings.ReplaceAll(searchString, ":", "#"),
		strings.Join(groups, "+"))
}

func (r *RedisGroupedStringCache) ElementCount(group string) int {
	res, err := r.rdb.DoCache(context.Background(), r.rdb.B().Scard().Key(r.groupKey(group)).Cache(), 10*time.Second).ToInt64()
	if err != nil {
		return 0
	}
	return int(res)
}

func (r *RedisGroupedStringCache) Contains(searchString string, groups []string) []string {
	start := time.Now()

	var result []string
	/*
		cTime := time.Minute
		entryKey := r.entryKey(searchString, groups)
		entryHit := fmt.Sprintf("%s-hit", entryKey)
		entryGrp := fmt.Sprintf("%s-grp", entryKey)

		cKey := r.groupKey(cacheGrp)
		b, err := r.rdb.DoCache(context.Background(), r.rdb.B().Hget().Key(cKey).Field(entryHit).Cache(), cTime).AsBool()
		if rueidis.IsRedisNil(err) {
			b = r.cacheMembers(searchString, cKey, entryHit, entryGrp, groups, false, cTime)
		} else if err != nil {
			panic(err)
		}

		if b {
			resp := r.rdb.DoCache(context.Background(), r.rdb.B().Hget().Key(cKey).Field(entryGrp).Cache(), cTime)
			s, err := resp.ToString()
			if err != nil {
				panic(err)
			}
			for _, ss := range strings.Split(s, ",") {
				result = append(result, ss)
			}
			go r.cacheMembers(searchString, cKey, entryKey, entryGrp, groups, true, cTime)
		}
	*/

	for _, group := range groups {
		r, err := r.rdb.DoCache(context.Background(), r.rdb.B().Sismember().Key(r.groupKey(group)).Member(searchString).Cache(), time.Minute).AsBool()
		if err != nil {
			panic(err)
		}
		if r {
			result = append(result, group)
			//break
		}
	}

	/*
		ig := len(groups)
		ch := make(chan string, ig)
		defer close(ch)
		for _, group := range groups {
			go r.isMember(searchString, group, ch)
		}
		g := ""
		for i := 0; i < ig; i++ {
			g = <-ch
			if len(g) > 0 {
				result = append(result, g)
			}
		}
	*/
	log.PrefixedLog("redis").Debugf("lookup for '%s': in groups: %v result: %v, duration %s", searchString, groups, result, durafmt.Parse(time.Since(start)).String())
	return result
}

func (r *RedisGroupedStringCache) isMember(searchString, group string, out chan<- string) {
	res, err := r.rdb.DoCache(context.Background(), r.rdb.B().Sismember().Key(r.groupKey(group)).Member(searchString).Cache(), time.Minute*10).AsBool()
	if err == nil && res {
		out <- group
		return
	}

	out <- ""
}

/*
func (r *RedisGroupedStringCache) cacheMembers(searchString, ckey, hk, hg string, groups []string, force bool, tt time.Duration) bool {
	keys := []string{ckey}
	for _, group := range groups {
		keys = append(keys, r.groupKey(group))
	}
	f := "0"
	if force {
		f = "1"
	}
	res, err := r.rdb.Do(context.Background(), r.rdb.B().Fcall().
		Function("blocky_cache_members").
		Numkeys(int64(len(keys))).
		Key(keys...).Arg(searchString, hk, hg, f, fmt.Sprintf("%d", int(tt.Seconds()))).
		Build()).AsBool()
	if err != nil {
		panic(err)
	}
	return res
}*/

func (r *RedisGroupedStringCache) Refresh(group string) GroupFactory {

	f := &RedisGroupFactory{
		rdb:       r.rdb,
		name:      group,
		cmds:      rueidis.Commands{},
		groupType: r.groupType,
	}

	f.cmds = append(f.cmds, r.rdb.B().Del().Key(f.groupKey()).Build(),
		r.rdb.B().Del().Key("blocky:cache:"+cacheGrp).Build())

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
		r.rdb.B().Sadd().Key(r.groupKey()).Member(entry).Build())
	r.cnt++
}

func (r *RedisGroupFactory) Count() int {
	return r.cnt
}

func (r *RedisGroupFactory) Finish() {
	_ = r.rdb.DoMulti(context.Background(), r.cmds...)
}

func (r *RedisGroupFactory) groupKey() string {
	return fmt.Sprintf("blocky:cache:%s:%s", r.groupType, r.name)
}
