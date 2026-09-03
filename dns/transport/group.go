package transport

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/service"

	mDNS "github.com/miekg/dns"
)

const (
	StrategyConcurrent = "concurrent"
	StrategyRoundRobin = "round_robin"
)

var _ adapter.DNSTransport = (*GroupTransport)(nil)

func RegisterGroup(registry *dns.TransportRegistry) {
	dns.RegisterTransport[option.GroupDNSServerOptions](registry, C.DNSTypeGroup, NewGroup)
}

type GroupTransport struct {
	dns.TransportAdapter

	ctx        context.Context
	logger     log.ContextLogger
	strategy   string
	serverTags []string
	rrIndex    atomic.Uint32
}

func NewGroup(ctx context.Context, logger log.ContextLogger, tag string, options option.GroupDNSServerOptions) (adapter.DNSTransport, error) {
	if len(options.Servers) == 0 {
		return nil, E.New("missing servers")
	}

	strategy := options.Strategy
	if strategy == "" {
		strategy = StrategyConcurrent
	}
	if strategy != StrategyConcurrent && strategy != StrategyRoundRobin {
		return nil, E.New("invalid strategy: ", strategy, ", must be 'concurrent' or 'round_robin'")
	}

	return &GroupTransport{
		TransportAdapter: dns.NewTransportAdapter(C.DNSTypeGroup, tag, options.Servers),
		ctx:              ctx,
		logger:           logger,
		strategy:         strategy,
		serverTags:       options.Servers,
	}, nil
}

func (t *GroupTransport) Start(stage adapter.StartStage) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	transportManager := service.FromContext[adapter.DNSTransportManager](t.ctx)
	if transportManager == nil {
		return E.New("missing DNS transport manager")
	}
	for _, tag := range t.serverTags {
		transport, loaded := transportManager.Transport(tag)
		if !loaded {
			return E.New("DNS server not found: ", tag)
		}
		if transport.Type() == C.DNSTypeGroup {
			return E.New("group cannot contain another group: ", tag)
		}
		if transport.Type() == C.DNSTypeFakeIP {
			return E.New("group cannot contain fakeip server: ", tag)
		}
	}
	return nil
}

func (t *GroupTransport) Close() error {
	return nil
}

func (t *GroupTransport) Reset() {
}

func (t *GroupTransport) Exchange(ctx context.Context, message *mDNS.Msg) (*mDNS.Msg, error) {
	done := make(chan struct{})
	var (
		response *mDNS.Msg
		err      error
	)
	t.ExchangeAsync(ctx, message, func(callbackResponse *mDNS.Msg, callbackErr error) {
		response = callbackResponse
		err = callbackErr
		close(done)
	})
	<-done
	return response, err
}

func (t *GroupTransport) ExchangeAsync(ctx context.Context, message *mDNS.Msg, callback func(response *mDNS.Msg, err error)) {
	transportManager := service.FromContext[adapter.DNSTransportManager](t.ctx)
	if transportManager == nil {
		callback(nil, E.New("missing DNS transport manager"))
		return
	}

	if t.strategy == StrategyRoundRobin {
		t.exchangeRoundRobin(ctx, transportManager, message, callback)
	} else {
		t.exchangeConcurrent(ctx, transportManager, message, callback)
	}
}

// exchangeConcurrent queries all servers concurrently and returns the fastest
// successful response. This is the original upstream behavior, kept unchanged.
func (t *GroupTransport) exchangeConcurrent(ctx context.Context, transportManager adapter.DNSTransportManager, message *mDNS.Msg, callback func(response *mDNS.Msg, err error)) {
	type result struct {
		response *mDNS.Msg
		tag      string
		err      error
	}

	resultCh := make(chan result, len(t.serverTags))
	ctx, cancel := context.WithCancel(ctx)

	for _, tag := range t.serverTags {
		transport, loaded := transportManager.Transport(tag)
		if !loaded {
			resultCh <- result{nil, tag, E.New("DNS server not found: ", tag)}
			continue
		}
		transport.ExchangeAsync(ctx, message.Copy(), func(response *mDNS.Msg, err error) {
			resultCh <- result{response, tag, err}
		})
	}

	go func() {
		var firstErr error
		for range t.serverTags {
			r := <-resultCh
			if r.err == nil && r.response != nil {
				t.logger.DebugContext(ctx, "fastest response from ", r.tag)
				cancel()
				callback(r.response, nil)
				return
			}
			if firstErr == nil && r.err != nil {
				firstErr = r.err
			}
		}

		cancel()
		if firstErr != nil {
			callback(nil, firstErr)
		} else {
			callback(nil, E.New("all DNS servers failed"))
		}
	}()
}

// exchangeRoundRobin implements true round-robin load balancing: each request
// queries only the preferred server (selected by round-robin index), and only
// falls back to the next server if the preferred one fails. This ensures that
// N concurrent requests are distributed across N servers for even load
// distribution, minimizing resource usage when servers have query limits.
func (t *GroupTransport) exchangeRoundRobin(ctx context.Context, transportManager adapter.DNSTransportManager, message *mDNS.Msg, callback func(response *mDNS.Msg, err error)) {
	serverCount := len(t.serverTags)
	startIndex := int(t.rrIndex.Add(1)-1) % serverCount

	// Use global timeout for the entire round-robin process, not per-server
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var lastErr error

	// Try each server in round-robin order, stopping at the first success.
	for i := 0; i < serverCount; i++ {
		// Check if we've exceeded the global timeout
		select {
		case <-ctx.Done():
			callback(nil, ctx.Err())
			return
		default:
		}

		index := (startIndex + i) % serverCount
		tag := t.serverTags[index]
		transport, loaded := transportManager.Transport(tag)
		if !loaded {
			t.logger.DebugContext(ctx, "round-robin: server not found: ", tag)
			continue
		}

		// Use async query to avoid blocking
		done := make(chan struct{})
		var response *mDNS.Msg
		var err error

		transport.ExchangeAsync(ctx, message.Copy(), func(resp *mDNS.Msg, e error) {
			response = resp
			err = e
			close(done)
		})

		select {
		case <-done:
			if err == nil && response != nil {
				t.logger.DebugContext(ctx, "round-robin success from ", tag)
				callback(response, nil)
				return
			}
			lastErr = err
			t.logger.DebugContext(ctx, "round-robin failed from ", tag, ": ", err, ", trying next")
		case <-ctx.Done():
			callback(nil, ctx.Err())
			return
		}
	}

	// All servers failed.
	if lastErr != nil {
		callback(nil, lastErr)
	} else {
		callback(nil, E.New("all DNS servers failed"))
	}
}
