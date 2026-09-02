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
	StrategyConcurrent  = "concurrent"
	StrategyRoundRobin  = "round_robin"
	defaultQueryTimeout = 5 * time.Second
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
	rrIndex    atomic.Uint32 // Round-robin index
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

// exchangeConcurrent 并发查询所有服务器，返回最快的响应
func (t *GroupTransport) exchangeConcurrent(ctx context.Context, transportManager adapter.DNSTransportManager, message *mDNS.Msg, callback func(response *mDNS.Msg, err error)) {
	type result struct {
		response *mDNS.Msg
		tag      string
		err      error
	}

	serverCount := len(t.serverTags)
	resultCh := make(chan result, serverCount)

	// Create a cancellable context with timeout
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Track if callback has been called to avoid double-callback
	var callbackOnce sync.Once

	// Launch all queries concurrently
	for _, tag := range t.serverTags {
		transport, loaded := transportManager.Transport(tag)
		if !loaded {
			resultCh <- result{nil, tag, E.New("DNS server not found: ", tag)}
			continue
		}

		// Create a timeout context for this specific query
		queryCtx, queryCancel := context.WithTimeout(ctx, defaultQueryTimeout)

		transport.ExchangeAsync(queryCtx, message.Copy(), func(response *mDNS.Msg, err error) {
			queryCancel()
			resultCh <- result{response, tag, err}
		})
	}

	// Goroutine to collect results and return the first successful one
	go func() {
		var firstErr error

		for i := 0; i < serverCount; i++ {
			select {
			case r := <-resultCh:
				if r.err == nil && r.response != nil {
					t.logger.DebugContext(ctx, "concurrent: fastest response from ", r.tag)
					cancel() // Cancel other queries
					callbackOnce.Do(func() {
						callback(r.response, nil)
					})
					return
				}

				if r.err != nil {
					t.logger.DebugContext(ctx, "concurrent: ", r.tag, " failed: ", r.err)
					if firstErr == nil {
						firstErr = r.err
					}
				}

			case <-ctx.Done():
				// Parent context cancelled
				callbackOnce.Do(func() {
					callback(nil, ctx.Err())
				})
				return
			}
		}

		// All servers responded, but all failed
		t.logger.DebugContext(ctx, "concurrent: all ", serverCount, " servers failed")
		callbackOnce.Do(func() {
			if firstErr != nil {
				callback(nil, firstErr)
			} else {
				callback(nil, E.New("all DNS servers failed"))
			}
		})
	}()
}

// exchangeRoundRobin 轮询查询，并发尝试多个服务器，优先使用轮询索引对应的服务器
func (t *GroupTransport) exchangeRoundRobin(ctx context.Context, transportManager adapter.DNSTransportManager, message *mDNS.Msg, callback func(response *mDNS.Msg, err error)) {
	serverCount := uint32(len(t.serverTags))

	// Atomically get and increment round-robin index
	startIndex := t.rrIndex.Add(1) - 1

	// Create a cancellable context with timeout
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		response *mDNS.Msg
		tag      string
		err      error
		index    uint32
	}

	resultCh := make(chan result, serverCount)
	var callbackOnce sync.Once

	// Launch all queries concurrently, prioritized by round-robin order
	for i := uint32(0); i < serverCount; i++ {
		index := (startIndex + i) % serverCount
		tag := t.serverTags[index]

		transport, loaded := transportManager.Transport(tag)
		if !loaded {
			resultCh <- result{nil, tag, E.New("DNS server not found: ", tag), index}
			continue
		}

		// Create a timeout context for this specific query
		queryCtx, queryCancel := context.WithTimeout(ctx, defaultQueryTimeout)

		go func(idx uint32, tr adapter.DNSTransport, queryTag string) {
			defer queryCancel()
			tr.ExchangeAsync(queryCtx, message.Copy(), func(response *mDNS.Msg, err error) {
				resultCh <- result{response, queryTag, err, idx}
			})
		}(index, transport, tag)
	}

	// Goroutine to collect results
	go func() {
		var (
			bestResponse *mDNS.Msg
			bestTag      string
			bestIndex    uint32 = serverCount // Higher than any valid index
			firstErr     error
			resultCount  int
		)

		for resultCount < int(serverCount) {
			select {
			case r := <-resultCh:
				resultCount++

				if r.err == nil && r.response != nil {
					// Success! Prefer lower index (closer to round-robin position)
					if r.index < bestIndex {
						bestResponse = r.response
						bestTag = r.tag
						bestIndex = r.index
						t.logger.DebugContext(ctx, "round-robin: success from ", r.tag, " (index ", r.index, ")")

						// If this is the highest priority server, return immediately
						if r.index == startIndex%serverCount {
							cancel()
							callbackOnce.Do(func() {
								callback(bestResponse, nil)
							})
							return
						}
					}
				} else {
					t.logger.DebugContext(ctx, "round-robin: ", r.tag, " failed: ", r.err)
					if firstErr == nil && r.err != nil {
						firstErr = r.err
					}
				}

			case <-ctx.Done():
				// Parent context cancelled
				callbackOnce.Do(func() {
					callback(nil, ctx.Err())
				})
				return
			}
		}

		// All queries completed
		if bestResponse != nil {
			t.logger.DebugContext(ctx, "round-robin: returning best response from ", bestTag)
			callbackOnce.Do(func() {
				callback(bestResponse, nil)
			})
		} else {
			t.logger.DebugContext(ctx, "round-robin: all servers failed")
			callbackOnce.Do(func() {
				if firstErr != nil {
					callback(nil, firstErr)
				} else {
					callback(nil, E.New("all DNS servers failed"))
				}
			})
		}
	}()
}
