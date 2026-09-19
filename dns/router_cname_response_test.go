package dns

import (
	"testing"

	"github.com/sagernet/sing-box/adapter"
	mDNS "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

func cnameMsg(name string, qtype uint16, target string) *mDNS.Msg {
	msg := &mDNS.Msg{Question: []mDNS.Question{{Name: mDNS.Fqdn(name), Qtype: qtype}}}
	msg.Answer = []mDNS.RR{chaseCNAME(name, target)}
	return msg
}

func TestMaybeChaseFillsAAAA(t *testing.T) {
	transport := &chaseScriptTransport{tag: "upstream", handler: func(name string, qtype uint16) []mDNS.RR {
		if name == "target.example." && qtype == mDNS.TypeAAAA {
			return []mDNS.RR{chaseAAAA("target.example", "2001:db8::1")}
		}
		return nil
	}}
	router := chaseTestRouter(true, transport)
	msg := cnameMsg("alias.example", mDNS.TypeAAAA, "target.example")
	merged := router.maybeChaseResponseCNAME(chaseTestContext("alias.example"), msg, msg, nil, adapter.DNSQueryOptions{})
	require.Len(t, merged.Answer, 2)
	require.IsType(t, &mDNS.AAAA{}, merged.Answer[1])
	require.Equal(t, int32(1), transport.queries.Load())
}

func TestMaybeChaseDisabled(t *testing.T) {
	transport := &chaseScriptTransport{tag: "upstream"}
	router := chaseTestRouter(false, transport)
	msg := cnameMsg("alias.example", mDNS.TypeAAAA, "target.example")
	merged := router.maybeChaseResponseCNAME(chaseTestContext("alias.example"), msg, msg, nil, adapter.DNSQueryOptions{})
	require.Len(t, merged.Answer, 1)
	require.Equal(t, int32(0), transport.queries.Load())
}

func TestMaybeChaseSkipsComplete(t *testing.T) {
	transport := &chaseScriptTransport{tag: "upstream"}
	router := chaseTestRouter(true, transport)
	msg := &mDNS.Msg{Question: []mDNS.Question{{Name: "alias.example.", Qtype: mDNS.TypeAAAA}}}
	msg.Answer = []mDNS.RR{
		chaseCNAME("alias.example", "target.example"),
		chaseAAAA("target.example", "2001:db8::1"),
	}
	merged := router.maybeChaseResponseCNAME(chaseTestContext("alias.example"), msg, msg, nil, adapter.DNSQueryOptions{})
	require.Len(t, merged.Answer, 2)
	require.Equal(t, int32(0), transport.queries.Load())
}

func TestChaseResponseCycleTerminates(t *testing.T) {
	transport := &chaseScriptTransport{tag: "upstream", handler: func(name string, qtype uint16) []mDNS.RR {
		if name == "target.example." {
			return []mDNS.RR{chaseCNAME("target.example", "alias.example")}
		}
		return nil
	}}
	router := chaseTestRouter(true, transport)
	msg := cnameMsg("alias.example", mDNS.TypeAAAA, "target.example")
	merged := router.maybeChaseResponseCNAME(chaseTestContext("alias.example"), msg, msg, nil, adapter.DNSQueryOptions{})
	require.Len(t, merged.Answer, 2)
	require.LessOrEqual(t, transport.queries.Load(), int32(2))
}

type fakeIPChaseTransport struct {
	chaseScriptTransport
}

func (t *fakeIPChaseTransport) Type() string { return "fakeip" }

func TestMaybeChaseSkipsFakeIP(t *testing.T) {
	transport := &fakeIPChaseTransport{chaseScriptTransport{tag: "fake", handler: func(name string, qtype uint16) []mDNS.RR {
		return []mDNS.RR{chaseAAAA("target.example", "2001:db8::1")}
	}}}
	router := chaseTestRouter(true, &transport.chaseScriptTransport)
	msg := cnameMsg("alias.example", mDNS.TypeAAAA, "target.example")
	merged := router.maybeChaseResponseCNAME(chaseTestContext("alias.example"), msg, msg, transport, adapter.DNSQueryOptions{})
	require.Len(t, merged.Answer, 1)
	require.Equal(t, int32(0), transport.queries.Load())
}

func TestExchangeAsyncChasesBareCNAME(t *testing.T) {
	transport := &chaseScriptTransport{tag: "upstream", handler: func(name string, qtype uint16) []mDNS.RR {
		if name == "target.example." && qtype == mDNS.TypeAAAA {
			return []mDNS.RR{chaseAAAA("target.example", "2001:db8::1")}
		}
		return nil
	}}
	router := chaseTestRouter(true, transport)
	message := &mDNS.Msg{
		MsgHdr:   mDNS.MsgHdr{Id: 7, RecursionDesired: true},
		Question: []mDNS.Question{{Name: "alias.example.", Qtype: mDNS.TypeAAAA, Qclass: mDNS.ClassINET}},
	}
	bare := &mDNS.Msg{Question: message.Question}
	bare.Answer = []mDNS.RR{chaseCNAME("alias.example", "target.example")}
	merged := router.maybeChaseResponseCNAME(chaseTestContext("alias.example"), message, bare, transport, adapter.DNSQueryOptions{})
	require.Len(t, merged.Answer, 2)
	require.IsType(t, &mDNS.AAAA{}, merged.Answer[1])
}
