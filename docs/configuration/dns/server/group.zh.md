---
icon: material/new-box
---

# Group

### 结构

```json
{
  "dns": {
    "servers": [
      {
        "type": "group",
        "tag": "dns-group",

        "strategy": "concurrent",
        "servers": [
          "dns-a",
          "dns-b"
        ],
        "exclude_threshold": 0
      }
    ]
  }
}
```

### 字段

#### strategy

DNS 查询策略。可用值：

- `concurrent`（默认）：并发查询所有服务器，返回最快的响应。
- `round_robin`：每次请求只查按轮询选中的一个服务器，无兜底。如果该服务器失败，直接返回错误。

#### exclude_threshold

单个服务器在15分钟窗口内允许的最大失败次数，超过后将暂时从组内剔除，适用于 `concurrent` 和 `round_robin`。下一个15分钟窗口开始时重新纳入。默认为 `0`（不启用）。

#### servers

==必填==

此组包含的 DNS 服务器 tag 列表。

限制：

- 组内不能包含另一个组。
- 组内不能包含 `fakeip` 类型的服务器。

查询时，根据 `strategy` 字段采用不同的行为：

- **concurrent**（默认）：组内所有服务器将被并发查询，最先返回的成功响应将作为结果使用。
- **round_robin**：每次请求只查询轮询选中的一个服务器，不兜底到其他服务器。如果失败，直接返回错误。
