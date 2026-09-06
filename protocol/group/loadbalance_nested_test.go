package group

import (
	"context"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	O "github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/urltest"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	N "github.com/sagernet/sing/common/network"

	"github.com/stretchr/testify/require"
)

func newLoadBalanceParent(history *urltest.HistoryStorage, members []adapter.Outbound) *LoadBalanceGroup {
	parent := &LoadBalanceGroup{
		ctx:      context.Background(),
		logger:   log.NewNOPFactory().NewLogger("test"),
		interval: 3 * time.Minute,
		history:  history,
	}
	parent.storeOutbounds(members)
	return parent
}

func TestIsSelfMeasuringGroup(t *testing.T) {
	require.True(t, isSelfMeasuringGroup(&URLTest{}))
	require.True(t, isSelfMeasuringGroup(&LoadBalance{}))
	require.False(t, isSelfMeasuringGroup(&Selector{}))
	require.False(t, isSelfMeasuringGroup(&preMatchTestOutbound{tag: "leaf"}))
}

func TestLoadBalanceParentReusesURLTestChild(t *testing.T) {
	history := urltest.NewHistoryStorage()
	leaf := &preMatchTestOutbound{tag: "leaf-01"}
	storeDelay(history, "leaf-01", 42)
	before := history.LoadURLTestHistory("leaf-01")
	child := &URLTest{
		Adapter: O.NewAdapter(C.TypeURLTest, "child", []string{N.NetworkTCP, N.NetworkUDP}, nil),
		group:   new(URLTestGroup),
	}
	child.group.selectedOutboundTCP.Store(adapter.Outbound(leaf))
	parent := newLoadBalanceParent(history, []adapter.Outbound{child})
	result, err := parent.urlTestLocked(context.Background(), false)
	require.NoError(t, err)
	require.Equal(t, map[string]uint16{"child": 42}, result)
	require.Same(t, before, history.LoadURLTestHistory("leaf-01"))
	require.Nil(t, history.LoadURLTestHistory("child"))
}

func TestLoadBalanceParentSkipsUnreadyChild(t *testing.T) {
	history := urltest.NewHistoryStorage()
	child := &URLTest{
		Adapter: O.NewAdapter(C.TypeURLTest, "child", []string{N.NetworkTCP, N.NetworkUDP}, nil),
		group:   new(URLTestGroup),
	}
	parent := newLoadBalanceParent(history, []adapter.Outbound{child})
	result, err := parent.urlTestLocked(context.Background(), false)
	require.NoError(t, err)
	require.Empty(t, result)
	require.Nil(t, history.LoadURLTestHistory("child"))
}

func TestLoadBalanceParentReusesLoadBalanceChild(t *testing.T) {
	history := urltest.NewHistoryStorage()
	fast := &preMatchTestOutbound{tag: "fast"}
	slow := &preMatchTestOutbound{tag: "slow"}
	storeDelay(history, "fast", 30)
	storeDelay(history, "slow", 200)
	beforeFast := history.LoadURLTestHistory("fast")
	child := &LoadBalance{
		Adapter: O.NewAdapter(C.TypeLoadBalance, "child-lb", []string{N.NetworkTCP, N.NetworkUDP}, nil),
		group: &LoadBalanceGroup{
			failures:    make(map[string]int),
			excluded:    make(map[string]bool),
			windowStart: time.Now(),
		},
	}
	child.group.storeOutbounds([]adapter.Outbound{adapter.Outbound(slow), adapter.Outbound(fast)})
	parent := newLoadBalanceParent(history, []adapter.Outbound{child})
	result, err := parent.urlTestLocked(context.Background(), false)
	require.NoError(t, err)
	require.Equal(t, map[string]uint16{"child-lb": 30}, result)
	require.Same(t, beforeFast, history.LoadURLTestHistory("fast"))
	require.Nil(t, history.LoadURLTestHistory("child-lb"))
}
