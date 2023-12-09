package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/0xERR0R/blocky/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func init() {
	log.Silence()
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "e2e Suite", Label("e2e"))
}

var _ = BeforeSuite(func(ctx context.Context) {
	SetDefaultEventuallyTimeout(5 * time.Second)
})

var _ = SynchronizedAfterSuite(func() {}, func(ctx context.Context) {
	for _, network := range networks {
		err := network.Remove(ctx)
		Expect(err).Should(Succeed())
	}
})
