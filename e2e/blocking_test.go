package e2e

import (
	"context"

	. "github.com/0xERR0R/blocky/e2e/util"

	. "github.com/0xERR0R/blocky/helpertest"
	"github.com/0xERR0R/blocky/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/testcontainers/testcontainers-go"
)

var _ = Describe("External lists and query blocking", func() {
	var (
		blocky testcontainers.Container
		err    error
		tmpDir *TmpFolder
	)

	BeforeEach(func(ctx context.Context) {
		tmpDir = NewTmpFolder("config")

		_, err = createDNSMokkaContainer(ctx, "moka", `A google/NOERROR("A 1.2.3.4 123")`)
		Expect(err).Should(Succeed())
	})
	Describe("List download on startup", func() {
		When("external blacklist ist not available", func() {
			Context("loading.strategy = blocking", func() {
				BeforeEach(func(ctx context.Context) {
					blocky, err = createBlockyContainer(ctx, tmpDir,
						"log:",
						"  level: warn",
						"upstreams:",
						"  groups:",
						"    default:",
						"      - moka",
						"blocking:",
						"  loading:",
						"    strategy: blocking",
						"  blackLists:",
						"    ads:",
						"      - http://wrong.domain.url/list.txt",
						"  clientGroupsBlock:",
						"    default:",
						"      - ads",
					)
					Expect(err).Should(Succeed())
				})

				It("should start with warning in log work without errors", func(ctx context.Context) {
					msg := util.NewMsgWithQuestion("google.com.", A)

					Expect(doDNSRequest(ctx, blocky, msg)).
						Should(
							SatisfyAll(
								BeDNSRecord("google.com.", A, "1.2.3.4"),
								HaveTTL(BeNumerically("==", 123)),
							))

					Expect(GetContainerLogs(ctx, blocky)).Should(ContainElement(ContainSubstring("cannot open source: ")))
				})
			})
			Context("loading.strategy = failOnError", func() {
				BeforeEach(func(ctx context.Context) {
					blocky, err = createBlockyContainer(ctx, tmpDir,
						"log:",
						"  level: warn",
						"upstreams:",
						"  groups:",
						"    default:",
						"      - moka",
						"blocking:",
						"  loading:",
						"    strategy: failOnError",
						"  blackLists:",
						"    ads:",
						"      - http://wrong.domain.url/list.txt",
						"  clientGroupsBlock:",
						"    default:",
						"      - ads",
					)
					Expect(err).Should(HaveOccurred())

					// check container exit status
					state, err := blocky.State(ctx)
					Expect(err).Should(Succeed())
					Expect(state.ExitCode).Should(Equal(1))
				})

				It("should fail to start", func(ctx context.Context) {
					Eventually(blocky.IsRunning, "5s", "2ms").Should(BeFalse())

					Expect(GetContainerLogs(ctx, blocky)).
						Should(ContainElement(ContainSubstring("Error: can't start server: 1 error occurred")))
				})
			})
		})
	})
	Describe("Query blocking against external blacklists", func() {
		const listFileName = "list.txt"
		When("external blacklists are defined and available", func() {
			BeforeEach(func(ctx context.Context) {
				httpserver, err := createHTTPServerContainer(ctx, "httpserver", listFileName, "blockeddomain.com")
				Expect(err).Should(Succeed())

				url, err := httpserver.GetFileURL(ctx, listFileName)
				Expect(err).Should(Succeed())

				blocky, err = createBlockyContainer(ctx, tmpDir,
					"log:",
					"  level: warn",
					"upstreams:",
					"  groups:",
					"    default:",
					"      - moka",
					"blocking:",
					"  blackLists:",
					"    ads:",
					"      - "+url,
					"  clientGroupsBlock:",
					"    default:",
					"      - ads",
				)

				Expect(err).Should(Succeed())
			})
			It("should download external list on startup and block queries", func(ctx context.Context) {
				msg := util.NewMsgWithQuestion("blockeddomain.com.", A)

				Expect(doDNSRequest(ctx, blocky, msg)).
					Should(
						SatisfyAll(
							BeDNSRecord("blockeddomain.com.", A, "0.0.0.0"),
							HaveTTL(BeNumerically("==", 6*60*60)),
						))

				Expect(GetContainerLogs(ctx, blocky)).Should(BeEmpty())
			})
		})
	})
})
