# Mustang 例行维护 checklist（每次同步上游时过一遍）

- [ ] 上游基线：确认 `reF1nd-testing` 有无新提交（`git fetch upstream` + `rev-list`），有则评估是否跟进。
- [ ] 第三方 port 回顾：`enfein/mbox` 的 `protocol/mieru/*`、`option/mieru.go` 有无更新；
  `github.com/enfein/mieru/v3` 有无值得跟的小版本。有则人工评估后手动合，
  禁止自动合并（mbox 自述非稳定，且其基线与本线 API 未必同拍）。
- [ ] 自有补丁回归：`protocol/group/loadbalance.go`（熔断/豁免）、
  `dns/transport/group.go`（round_robin）与新基线有无语义冲突；
  跑 `protocol/group` + `dns` 单测 + 三平台构建。
