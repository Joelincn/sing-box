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

        "servers": [
          "dns-a",
          "dns-b"
        ]
      }
    ]
  }
}
```

### 字段

#### strategy

DNS 查询策略。可用值：

- `concurrent`（默认）：并发查询所有服务器，返回最快的响应。
- `round_robin`：按轮询顺序查询服务器，失败时自动切换。

#### servers

==必填==

此组包含的 DNS 服务器 tag 列表。

限制：

- 组内不能包含另一个组。
- 组内不能包含 `fakeip` 类型的服务器。

查询时，根据 `strategy` 字段采用不同的行为：

- **concurrent**（默认）：组内所有服务器将被并发查询，最先返回的成功响应将作为结果使用。
- **round_robin**：按轮询顺序查询服务器。如果某个服务器失败，将自动尝试下一个服务器。
