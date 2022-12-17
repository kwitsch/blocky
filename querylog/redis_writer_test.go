package querylog

import (
	"time"

	"github.com/0xERR0R/blocky/config"
	"github.com/alicebob/miniredis/v2"
	. "github.com/onsi/gomega"

	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("RedisWriter", func() {
	var (
		redisServer *miniredis.Miniredis
		writer      *RedisWriter
		err         error
	)

	BeforeEach(func() {
		redisServer, err = miniredis.Run()
		Expect(err).Should(Succeed())
		DeferCleanup(redisServer.Close)

		cfg := config.QueryLogConfig{
			Type:             config.QueryLogTypeRedis,
			LogRetentionDays: 1,
		}

		redisCfg := config.RedisConfig{
			Address:  redisServer.Addr(),
			Database: 1,
		}

		writer = NewRedisWriter(cfg, &redisCfg, time.Millisecond*100)
	})

	When("New log entry was created", func() {
		It("should be persisted in the database", func() {
			// one entry with now as timestamp
			writer.Write(&LogEntry{
				Start:      time.Now(),
				DurationMs: 20,
			})

			// one entry before 2 days
			writer.Write(&LogEntry{
				Start:      time.Now().AddDate(0, 0, -3),
				DurationMs: 20,
			})

			// force write
			Expect(writer.doDBWrite()).Should(Succeed())

			// 2 entries in the database
			Eventually(func() int64 {
				result := writer.client.DBSize(writer.ctx)
				Expect(result.Err()).Should(Succeed())

				return result.Val()
			}, "5s").Should(BeNumerically("==", 2))

			redisServer.FastForward(12 * time.Hour)

			// now only 1 entry in the database
			Eventually(func() (res int64) {
				result := writer.client.DBSize(writer.ctx)
				Expect(result.Err()).Should(Succeed())

				return result.Val()
			}, "5s").Should(BeNumerically("==", 1))
		})
	})
})
