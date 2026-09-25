package group

import (
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/route/rule"
	E "github.com/sagernet/sing/common/exceptions"
)

type interruptExcluder struct {
	entries      [][]rule.RuleItem
	ruleSetItems []*rule.RuleSetItem
}

func newInterruptExcluder(router adapter.Router, options []option.InterruptExcludeOptions) (*interruptExcluder, error) {
	excluder := &interruptExcluder{}
	for i, entryOptions := range options {
		var items []rule.RuleItem
		if len(entryOptions.DomainSuffix) > 0 {
			item, err := rule.NewDomainItem(nil, entryOptions.DomainSuffix, router.DefaultDomainMatchStrategy())
			if err != nil {
				excluder.Close()
				return nil, E.Cause(err, "interrupt_exclude[", i, "]")
			}
			items = append(items, item)
		}
		if len(entryOptions.PackageName) > 0 {
			items = append(items, rule.NewPackageNameItem(entryOptions.PackageName))
		}
		if len(entryOptions.ProcessName) > 0 {
			items = append(items, rule.NewProcessItem(entryOptions.ProcessName))
		}
		if len(entryOptions.ProcessPath) > 0 {
			items = append(items, rule.NewProcessPathItem(entryOptions.ProcessPath))
		}
		if len(entryOptions.Port) > 0 {
			items = append(items, rule.NewPortItem(false, entryOptions.Port))
		}
		if len(entryOptions.RuleSet) > 0 {
			item := rule.NewRuleSetItem(router, entryOptions.RuleSet, false, false)
			if err := item.Start(); err != nil {
				excluder.Close()
				return nil, E.Cause(err, "interrupt_exclude[", i, "]")
			}
			items = append(items, item)
			excluder.ruleSetItems = append(excluder.ruleSetItems, item)
		}
		if len(items) == 0 {
			excluder.Close()
			return nil, E.New("interrupt_exclude[", i, "]: empty item is not allowed")
		}
		excluder.entries = append(excluder.entries, items)
	}
	return excluder, nil
}

func (e *interruptExcluder) Close() error {
	for _, item := range e.ruleSetItems {
		_ = item.Close()
	}
	e.ruleSetItems = nil
	return nil
}

func (e *interruptExcluder) isProtected(metadata *adapter.InboundContext) bool {
	if e == nil || metadata == nil {
		return false
	}
	for _, entry := range e.entries {
		protected := true
		for _, item := range entry {
			if !item.Match(metadata) {
				protected = false
				break
			}
		}
		if protected {
			return true
		}
	}
	return false
}
