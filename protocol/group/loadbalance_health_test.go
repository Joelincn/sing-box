package group

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/urltest"
	"github.com/sagernet/sing-box/log"

	"github.com/stretchr/testify/require"
)

func newHealthTestGroup(history *urltest.HistoryStorage, members []adapter.Outbound) *LoadBalanceGroup {
	return &LoadBalanceGroup{
		ctx:              context.Background(),
		tag:              "health",
		logger:           log.NewNOPFactory().Logger(),
		interval:         3 * time.Minute,
		history:          history,
		excludeThreshold: 2,
		failures:         make(map[string]int),
		excluded:         make(map[string]bool),
		windowStart:      time.Now(),
		trackedConns:     make(map[string]map[io.Closer]struct{}),
		outbounds:        members,
	}
}

func TestLoadBalanceExcludeThreshold(t *testing.T) {
	history := urltest.NewHistoryStorage()
	history.StoreURLTestHistory("leaf", &adapter.URLTestHistory{Time: time.Now(), Delay: 10})
	group := newHealthTestGroup(history, []adapter.Outbound{&preMatchTestOutbound{tag: "leaf"}})
	require.True(t, group.isAvailable(&preMatchTestOutbound{tag: "leaf"}))
	group.recordFailure("leaf", errors.New("boom"))
	require.True(t, group.isAvailable(&preMatchTestOutbound{tag: "leaf"}))
	require.False(t, group.isExcluded("leaf"))
	group.recordFailure("leaf", errors.New("boom"))
	require.True(t, group.isExcluded("leaf"))
	require.False(t, group.isAvailable(&preMatchTestOutbound{tag: "leaf"}))
}

func TestLoadBalanceRecordFailureIgnoresCanceled(t *testing.T) {
	history := urltest.NewHistoryStorage()
	group := newHealthTestGroup(history, nil)
	group.recordFailure("leaf", context.Canceled)
	require.Empty(t, group.failures)
}

func TestLoadBalanceHealthDisabledByDefault(t *testing.T) {
	history := urltest.NewHistoryStorage()
	group := &LoadBalanceGroup{history: history, windowStart: time.Now()}
	group.recordFailure("leaf", errors.New("boom"))
	require.False(t, group.isExcluded("leaf"))
	require.False(t, group.healthEnabled())
	require.False(t, group.trackingEnabled())
}

func TestIsDirectLeafForHealth(t *testing.T) {
	require.True(t, isDirectLeafForHealth(&preMatchTestOutbound{tag: "leaf"}))
	require.True(t, isDirectLeafForHealth(&Selector{}))
	require.False(t, isDirectLeafForHealth(&URLTest{}))
	require.False(t, isDirectLeafForHealth(&LoadBalance{}))
}
