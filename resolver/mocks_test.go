package resolver

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/0xERR0R/blocky/config"
	"github.com/0xERR0R/blocky/util"

	"github.com/0xERR0R/blocky/model"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/mock"
)

type mockResolver struct {
	mock.Mock
	NextResolver

	ResolveFn  func(req *model.Request) (*model.Response, error)
	ResponseFn func(req *dns.Msg) *dns.Msg
	AnswerFn   func(qType dns.Type, qName string) (*dns.Msg, error)
}

func (r *mockResolver) Configuration() []string {
	args := r.Called()

	return args.Get(0).([]string)
}

func (r *mockResolver) Resolve(req *model.Request) (*model.Response, error) {
	args := r.Called(req)

	if r.ResolveFn != nil {
		return r.ResolveFn(req)
	}

	if r.ResponseFn != nil {
		return &model.Response{
			Res:    r.ResponseFn(req.Req),
			Reason: "",
			RType:  model.ResponseTypeRESOLVED,
		}, nil
	}

	if r.AnswerFn != nil {
		for _, question := range req.Req.Question {
			answer, err := r.AnswerFn(dns.Type(question.Qtype), question.Name)
			if err != nil {
				return nil, fmt.Errorf("AnswerFn error: %w", err)
			}

			if answer != nil {
				return &model.Response{
					Res:    answer,
					Reason: "",
					RType:  model.ResponseTypeRESOLVED,
				}, nil
			}
		}

		response := new(dns.Msg)
		response.SetRcode(req.Req, dns.RcodeBadName)

		return &model.Response{
			Res:    response,
			Reason: "",
			RType:  model.ResponseTypeRESOLVED,
		}, nil
	}

	resp, ok := args.Get(0).(*model.Response)
	if ok {
		return resp, args.Error(1)
	}

	return nil, args.Error(1)
}

var (
	autoAnswerIPv4 = net.IPv4(127, 0, 0, 1)
	autoAnswerIPv6 = net.IPv6loopback
)

// autoAnswer provides a valid fake answer.
//
// To be used as a value for `mockResolver.AnswerFn`.
func autoAnswer(qType dns.Type, qName string) (*dns.Msg, error) {
	var ip net.IP

	switch uint16(qType) {
	case dns.TypeA:
		ip = autoAnswerIPv4
	case dns.TypeAAAA:
		ip = autoAnswerIPv6
	default:
		return nil, fmt.Errorf("autoAnswer not implemented for qType=%s", dns.TypeToString[uint16(qType)])
	}

	return util.NewMsgWithAnswer(qName, 60, qType, ip.String())
}

// newTestBootstrap creates a test Bootstrap
func newTestBootstrap(response *dns.Msg) *Bootstrap {
	bootstrapUpstream := &mockResolver{}

	b, err := NewBootstrap(&config.Config{})
	util.FatalOnError("can't create bootstrap", err)

	b.resolver = bootstrapUpstream
	b.bootstraped = bootstrapedResolvers{bootstrapUpstream: []net.IP{}}

	if response != nil {
		bootstrapUpstream.
			On("Resolve", mock.Anything).
			Return(&model.Response{Res: response}, nil)
	}

	return b
}

// newTestDOHUpstream creates a test DoH Upstream
func newTestDOHUpstream(fn func(request *dns.Msg) (response *dns.Msg),
	reqFn ...func(w http.ResponseWriter),
) config.Upstream {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)

		util.FatalOnError("can't read request: ", err)

		msg := new(dns.Msg)
		err = msg.Unpack(body)
		util.FatalOnError("can't deserialize message: ", err)

		response := fn(msg)
		response.SetReply(msg)

		b, err := response.Pack()

		util.FatalOnError("can't serialize message: ", err)

		w.Header().Set("content-type", "application/dns-message")

		for _, f := range reqFn {
			if f != nil {
				f(w)
			}
		}
		_, err = w.Write(b)

		util.FatalOnError("can't write response: ", err)
	}))

	upstream, err := config.ParseUpstream(server.URL)

	util.FatalOnError("can't resolve address: ", err)

	return upstream
}

type mockDialer struct {
	mock.Mock
}

func newMockDialer() *mockDialer {
	return &mockDialer{}
}

func (d *mockDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	d.Called(ctx, network, addr)

	return aMockConn, nil
}

var aMockConn = &mockConn{}

type mockConn struct{}

func (c *mockConn) Read(b []byte) (n int, err error) {
	panic("not implemented")
}

func (c *mockConn) Write(b []byte) (n int, err error) {
	panic("not implemented")
}

func (c *mockConn) Close() error {
	panic("not implemented")
}

func (c *mockConn) LocalAddr() net.Addr {
	panic("not implemented")
}

func (c *mockConn) RemoteAddr() net.Addr {
	panic("not implemented")
}

func (c *mockConn) SetDeadline(t time.Time) error {
	panic("not implemented")
}

func (c *mockConn) SetReadDeadline(t time.Time) error {
	panic("not implemented")
}

func (c *mockConn) SetWriteDeadline(t time.Time) error {
	panic("not implemented")
}
