package group

import (
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/urltest"

	"github.com/stretchr/testify/require"
)

func storeDelay(history *urltest.HistoryStorage, tag string, delay uint16) {
	history.StoreURLTestHistory(tag, &adapter.URLTestHistory{Time: time.Now(), Delay: delay})
}

func TestReuseGroupDelayURLTestChild(t *testing.T) {
	history := urltest.NewHistoryStorage()
	leaf := &preMatchTestOutbound{tag: "leaf-01"}
	storeDelay(history, "leaf-01", 42)
	child := &URLTest{group: new(URLTestGroup)}
	child.group.selectedOutboundTCP.Store(adapter.Outbound(leaf))
	delay, ok := reuseGroupDelay(child, history, time.Minute)
	require.True(t, ok)
	require.Equal(t, uint16(42), delay)
	require.True(t, groupMemberReady(child, history))
}

func TestReuseGroupDelayURLTestChildStale(t *testing.T) {
	history := urltest.NewHistoryStorage()
	leaf := &preMatchTestOutbound{tag: "leaf-01"}
	history.StoreURLTestHistory("leaf-01", &adapter.URLTestHistory{Time: time.Now().Add(-time.Hour), Delay: 42})
	child := &URLTest{group: new(URLTestGroup)}
	child.group.selectedOutboundTCP.Store(adapter.Outbound(leaf))
	_, ok := reuseGroupDelay(child, history, time.Minute)
	require.False(t, ok)
	require.True(t, groupMemberReady(child, history))
}

func TestReuseGroupDelayURLTestChildUnready(t *testing.T) {
	history := urltest.NewHistoryStorage()
	child := &URLTest{group: new(URLTestGroup)}
	_, ok := reuseGroupDelay(child, history, time.Minute)
	require.False(t, ok)
	require.False(t, groupMemberReady(child, history))
}

func TestReuseGroupDelayLoadBalanceChild(t *testing.T) {
	history := urltest.NewHistoryStorage()
	fast := &preMatchTestOutbound{tag: "fast"}
	slow := &preMatchTestOutbound{tag: "slow"}
	storeDelay(history, "fast", 30)
	storeDelay(history, "slow", 200)
	child := &LoadBalance{group: &LoadBalanceGroup{
		failures:   make(map[string]int),
		excluded:   make(map[string]bool),
		windowStart: time.Now(),
	}}
	child.group.storeOutbounds([]adapter.Outbound{adapter.Outbound(slow), adapter.Outbound(fast)})
	delay, ok := reuseGroupDelay(child, history, time.Minute)
	require.True(t, ok)
	require.Equal(t, uint16(30), delay)
	require.True(t, groupMemberReady(child, history))
}

func TestReuseGroupDelayLoadBalanceChildSkipsExcluded(t *testing.T) {
	history := urltest.NewHistoryStorage()
	bad := &preMatchTestOutbound{tag: "bad"}
	good := &preMatchTestOutbound{tag: "good"}
	storeDelay(history, "bad", 10)
	storeDelay(history, "good", 80)
	child := &LoadBalance{group: &LoadBalanceGroup{
		excludeThreshold: 1,
		failures:         make(map[string]int),
		excluded:         map[string]bool{"bad": true},
		windowStart:      time.Now(),
	}}
	child.group.storeOutbounds([]adapter.Outbound{adapter.Outbound(bad), adapter.Outbound(good)})
	delay, ok := reuseGroupDelay(child, history, time.Minute)
	require.True(t, ok)
	require.Equal(t, uint16(80), delay)
}

func TestReuseGroupDelayLeaf(t *testing.T) {
	history := urltest.NewHistoryStorage()
	leaf := &preMatchTestOutbound{tag: "leaf-01"}
	storeDelay(history, "leaf-01", 42)
	_, ok := reuseGroupDelay(adapter.Outbound(leaf), history, time.Minute)
	require.False(t, ok)
	require.True(t, groupMemberReady(adapter.Outbound(leaf), history))
}
