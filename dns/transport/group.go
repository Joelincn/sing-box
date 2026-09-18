package transport

import (
	"context"
	"sync"
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

	excludeWindow = 15 * time.Minute
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

	excludeThreshold int
	mu               sync.Mutex
	failures         []int
	excluded         []bool
	windowStart      time.Time
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
	if options.ExcludeThreshold < 0 {
		return nil, E.New("invalid exclude_threshold: must be >= 0")
	}

	return &GroupTransport{
		TransportAdapter: dns.NewTransportAdapter(C.DNSTypeGroup, tag, options.Servers),
		ctx:              ctx,
		logger:           logger,
		strategy:         strategy,
		serverTags:       options.Servers,
		excludeThreshold: options.ExcludeThreshold,
		failures:         make([]int, len(options.Servers)),
		excluded:         make([]bool, len(options.Servers)),
		windowStart:      time.Now(),
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
	if t.excludeThreshold <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range t.serverTags {
		t.failures[i] = 0
		t.excluded[i] = false
	}
	t.windowStart = time.Now()
}

func (t *GroupTransport) healthEnabled() bool {
	return t.excludeThreshold > 0
}

func (t *GroupTransport) rotateWindowLocked(now time.Time) {
	if now.Sub(t.windowStart) >= excludeWindow {
		for i := range t.serverTags {
			t.failures[i] = 0
			t.excluded[i] = false
		}
		t.windowStart = now
	}
}

func (t *GroupTransport) activeTags() []string {
	if !t.healthEnabled() {
		return t.serverTags
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rotateWindowLocked(time.Now())
	active := make([]string, 0, len(t.serverTags))
	for i, tag := range t.serverTags {
		if !t.excluded[i] {
			active = append(active, tag)
		}
	}
	if len(active) == 0 {
		return t.serverTags
	}
	return active
}

func (t *GroupTransport) recordResult(tag string, err error, response *mDNS.Msg) {
	if !t.healthEnabled() {
		return
	}
	if err == nil && response != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rotateWindowLocked(time.Now())
	for i, candidate := range t.serverTags {
		if candidate != tag {
			continue
		}
		t.failures[i]++
		if !t.excluded[i] && t.failures[i] >= t.excludeThreshold {
			t.excluded[i] = true
			t.logger.WarnContext(t.ctx, "group exclude DNS server ", tag, ": ", t.failures[i], " failures in 15m")
		}
		return
	}
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

	tags := t.activeTags()
	for _, tag := range tags {
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
		for range tags {
			r := <-resultCh
			t.recordResult(r.tag, r.err, r.response)
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

// exchangeRoundRobin implements true round-robin load balancing (like mosdns-x):
// each request queries only the preferred server (selected by round-robin index).
// No fallback - if the preferred server fails, the error is returned immediately.
// This ensures that N concurrent requests are distributed across N servers
// for even load distribution, minimizing resource usage when servers have
// query limits.
func (t *GroupTransport) exchangeRoundRobin(ctx context.Context, transportManager adapter.DNSTransportManager, message *mDNS.Msg, callback func(response *mDNS.Msg, err error)) {
	tags := t.activeTags()
	index := int(t.rrIndex.Add(1)-1) % len(tags)
	tag := tags[index]

	transport, loaded := transportManager.Transport(tag)
	if !loaded {
		callback(nil, E.New("round-robin: server not found: ", tag))
		return
	}

	// Query only the preferred server, no fallback
	transport.ExchangeAsync(ctx, message.Copy(), func(response *mDNS.Msg, err error) {
		t.recordResult(tag, err, response)
		if err == nil && response != nil {
			t.logger.DebugContext(ctx, "round-robin success from ", tag)
		}
		callback(response, err)
	})
}
