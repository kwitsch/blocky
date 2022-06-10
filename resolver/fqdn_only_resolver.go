package resolver

import (
	"fmt"
	"strings"

	"github.com/0xERR0R/blocky/config"
	"github.com/0xERR0R/blocky/model"
	"github.com/0xERR0R/blocky/util"
	"github.com/miekg/dns"
)

type FqdnOnlyResolver struct {
	NextResolver
	enabled bool
}

func NewFqdnOnlyResolver(cfg config.Config) ChainedResolver {
	return &FqdnOnlyResolver{
		enabled: cfg.FqdnOnly,
	}
}

func (r *FqdnOnlyResolver) Resolve(request *model.Request) (*model.Response, error) {
	if r.enabled {
		domainFromQuestion := util.ExtractDomain(request.Req.Question[0])
		if !strings.Contains(domainFromQuestion, ".") {
			response := new(dns.Msg)
			response.Rcode = dns.RcodeNameError

			reason := fmt.Sprintf("NOTFQDN (%s)", domainFromQuestion)

			return &model.Response{Res: response, RType: model.ResponseTypeNOTFQDN, Reason: reason}, nil
		}
	}

	return r.next.Resolve(request)
}

func (r *FqdnOnlyResolver) Configuration() (result []string) {
	if r.enabled {
		result = []string{"activated"}
	} else {
		result = []string{"deactivated"}
	}

	return result
}
