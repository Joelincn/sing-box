package dns

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"

	mDNS "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

type chaseScriptTransport struct {
	tag     string
	handler func(name string, qtype uint16) []mDNS.RR
	queries atomic.Int32
}

func (t *chaseScriptTransport) Start(stage adapter.StartStage) error { return nil }
func (t *chaseScriptTransport) Close() error                         { return nil }
func (t *chaseScriptTransport) Type() string                         { return "fake" }
func (t *chaseScriptTransport) Tag() string                          { return t.tag }
func (t *chaseScriptTransport) Dependencies() []string               { return nil }
func (t *chaseScriptTransport) Reset()                               {}
func (t *chaseScriptTransport) Exchange(ctx context.Context, message *mDNS.Msg) (*mDNS.Msg, error) {
	t.queries.Add(1)
	question := message.Question[0]
	response := new(mDNS.Msg)
	response.SetReply(message)
	response.Answer = t.handler(question.Name, question.Qtype)
	return response, nil
}
func (t *chaseScriptTransport) ExchangeAsync(ctx context.Context, message *mDNS.Msg, callback func(response *mDNS.Msg, err error)) {
	response, err := t.Exchange(ctx, message)
	callback(response, err)
}

func chaseTestRouter(followCNAME bool, transport *chaseScriptTransport) *Router {
	return &Router{
		ctx:         context.Background(),
		logger:      log.NewNOPFactory().Logger(),
		transport:   &fakeDNSTransportManager{transports: map[string]adapter.DNSTransport{"upstream": transport}, defaultTransport: transport},
		followCNAME: followCNAME,
		client: NewClient(ClientOptions{
			Context:      context.Background(),
			DisableCache: true,
			Logger:       log.NewNOPFactory().Logger(),
		}),
	}
}

func chaseTestRules(t *testing.T, domains ...string) []adapter.DNSRule {
	return raceTestRules(t, []option.DNSRule{{
		Type: "",
		DefaultOptions: option.DefaultDNSRule{
			RawDefaultDNSRule: option.RawDefaultDNSRule{Domain: domains},
			DNSRuleAction: option.DNSRuleAction{
				Action:       C.RuleActionTypeRoute,
				RouteOptions: option.DNSRouteActionOptions{Server: "upstream"},
			},
		},
	}})
}

func chaseTestContext(domain string) context.Context {
	ctx, metadata := adapter.ExtendContext(context.Background())
	metadata.Domain = domain
	return adapter.WithContext(ctx, metadata)
}

func chaseCNAME(name, target string) mDNS.RR {
	return &mDNS.CNAME{
		Hdr:    mDNS.RR_Header{Name: mDNS.Fqdn(name), Rrtype: mDNS.TypeCNAME, Class: mDNS.ClassINET, Ttl: 300},
		Target: mDNS.Fqdn(target),
	}
}

func chaseA(name, ip string) mDNS.RR {
	return &mDNS.A{
		Hdr: mDNS.RR_Header{Name: mDNS.Fqdn(name), Rrtype: mDNS.TypeA, Class: mDNS.ClassINET, Ttl: 300},
		A:   net.ParseIP(ip).To4(),
	}
}

func chaseAAAA(name, ip string) mDNS.RR {
	return &mDNS.AAAA{
		Hdr:  mDNS.RR_Header{Name: mDNS.Fqdn(name), Rrtype: mDNS.TypeAAAA, Class: mDNS.ClassINET, Ttl: 300},
		AAAA: net.ParseIP(ip).To16(),
	}
}

func TestLookupChaseFillsIPv6(t *testing.T) {
	transport := &chaseScriptTransport{tag: "upstream", handler: func(name string, qtype uint16) []mDNS.RR {
		switch {
		case name == "alias.example." && qtype == mDNS.TypeAAAA:
			return []mDNS.RR{chaseCNAME("alias.example", "target.example")}
		case name == "target.example." && qtype == mDNS.TypeAAAA:
			return []mDNS.RR{chaseAAAA("target.example", "2001:db8::1")}
		case name == "alias.example." && qtype == mDNS.TypeA:
			return []mDNS.RR{chaseCNAME("alias.example", "target.example"), chaseA("target.example", "192.0.2.1")}
		}
		return nil
	}}
	router := chaseTestRouter(true, transport)
	rules := chaseTestRules(t, "alias.example", "target.example")
	addrs, err := router.lookupWithRules(chaseTestContext("alias.example"), rules, "alias.example", adapter.DNSQueryOptions{})
	require.NoError(t, err)
	require.Len(t, addrs, 2)
	require.True(t, addrs[0].Is4())
	require.True(t, addrs[1].Is6())
	require.Equal(t, int32(3), transport.queries.Load())
}

func TestLookupChaseDisabled(t *testing.T) {
	transport := &chaseScriptTransport{tag: "upstream", handler: func(name string, qtype uint16) []mDNS.RR {
		if name == "alias.example." && qtype == mDNS.TypeAAAA {
			return []mDNS.RR{chaseCNAME("alias.example", "target.example")}
		}
		return nil
	}}
	router := chaseTestRouter(false, transport)
	rules := chaseTestRules(t, "alias.example", "target.example")
	addrs, err := router.lookupWithRules(chaseTestContext("alias.example"), rules, "alias.example", adapter.DNSQueryOptions{})
	require.NoError(t, err)
	require.Empty(t, addrs)
	require.Equal(t, int32(2), transport.queries.Load())
}

func TestLookupNoChaseWhenComplete(t *testing.T) {
	transport := &chaseScriptTransport{tag: "upstream", handler: func(name string, qtype uint16) []mDNS.RR {
		if name == "alias.example." && qtype == mDNS.TypeAAAA {
			return []mDNS.RR{chaseCNAME("alias.example", "target.example"), chaseAAAA("target.example", "2001:db8::1")}
		}
		return nil
	}}
	router := chaseTestRouter(true, transport)
	rules := chaseTestRules(t, "alias.example", "target.example")
	addrs, err := router.lookupWithRules(chaseTestContext("alias.example"), rules, "alias.example", adapter.DNSQueryOptions{})
	require.NoError(t, err)
	require.Len(t, addrs, 1)
	require.Equal(t, int32(2), transport.queries.Load())
}

func TestLookupChaseCycleTerminates(t *testing.T) {
	transport := &chaseScriptTransport{tag: "upstream", handler: func(name string, qtype uint16) []mDNS.RR {
		if name == "a.example." {
			return []mDNS.RR{chaseCNAME("a.example", "b.example")}
		}
		if name == "b.example." {
			return []mDNS.RR{chaseCNAME("b.example", "a.example")}
		}
		return nil
	}}
	router := chaseTestRouter(true, transport)
	rules := chaseTestRules(t, "a.example", "b.example")
	addrs, err := router.lookupWithRules(chaseTestContext("a.example"), rules, "a.example", adapter.DNSQueryOptions{})
	require.NoError(t, err)
	require.Empty(t, addrs)
	require.LessOrEqual(t, transport.queries.Load(), int32(4))
}

func TestLookupChaseDepthCapped(t *testing.T) {
	transport := &chaseScriptTransport{tag: "upstream", handler: func(name string, qtype uint16) []mDNS.RR {
		var n int
		if _, err := fmt.Sscanf(name, "chain%d.example.", &n); err != nil {
			return nil
		}
		return []mDNS.RR{chaseCNAME(trimDot(name), fmt.Sprintf("chain%d.example", n+1))}
	}}
	domains := []string{"chain1.example", "chain2.example", "chain3.example", "chain4.example", "chain5.example", "chain6.example", "chain7.example", "chain8.example", "chain9.example", "chain10.example", "chain11.example", "chain12.example"}
	router := chaseTestRouter(true, transport)
	rules := chaseTestRules(t, domains...)
	addrs, err := router.lookupWithRules(chaseTestContext("chain1.example"), rules, "chain1.example", adapter.DNSQueryOptions{Strategy: C.DomainStrategyIPv6Only})
	require.NoError(t, err)
	require.Empty(t, addrs)
	require.LessOrEqual(t, transport.queries.Load(), int32(10))
}

func trimDot(name string) string {
	if len(name) > 0 && name[len(name)-1] == '.' {
		return name[:len(name)-1]
	}
	return name
}
