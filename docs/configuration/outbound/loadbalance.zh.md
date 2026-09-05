### 结构

```json
{
  "type": "loadbalance",
  "tag": "balance",
  "strategy": "round-robin",

  "outbounds": [
    "proxy-a",
    "proxy-b",
    "proxy-c"
  ],
  "providers": [
    "provider-a",
    "provider-b"
  ],
  "exclude": "",
  "include": "",
  "url": "",
  "interval": "",
  "idle_timeout": "",
  "ttl": "10m",
  "use_all_providers": false,
  "exclude_threshold": 0,
  "interrupt_exist_connections": false,
  "interrupt_exclude": []
}
```

!!! note ""

    当内容只有一项时，可以忽略 JSON 数组 [] 标签。

### 字段

#### strategy

负载均衡策略。

* `round-robin` 将在策略组内的不同代理节点之间分配所有请求。

* `consistent-hashing` 将具有相同 `目标地址` 的请求分配给策略组内的同一代理节点。

* `sticky-sessions`：具有相同 `源地址` 和 `目标地址` 的请求将被导向策略组内的同一代理节点，缓存过期时间为指定的 ttl。

!!! note
    当 `目标地址` 是域名时，使用顶级域名匹配。

#### outbounds

用于测试的出站标签列表。

#### providers

用于测试的[订阅](/zh/configuration/provider)标签列表。

#### exclude

排除 `providers` 节点的正则表达式。

#### include

包含 `providers` 节点的正则表达式。

#### url

用于测试的链接。默认使用 `https://www.gstatic.com/generate_204`。

#### interval

测试间隔。 默认使用 `3m`。

#### idle_timeout

空闲超时。默认使用 `30m`。

#### ttl

用于 `sticky-sessions` 策略超时的生存时间。默认使用 `10m`。

#### use_all_providers

是否使用所有提供者。默认使用 `false`。

#### exclude_threshold

单个出站在15分钟窗口内允许的最大拨号失败次数，超过后将暂时从负载均衡中剔除，适用于所有策略（`round-robin`、`consistent-hashing`、`sticky-sessions`）。只统计真实业务拨号失败，调用方主动取消的不计；URL 测试失败只影响可用性判定，不计入。下一个15分钟窗口开始时重新纳入。默认为 `0`（不启用）。

#### interrupt_exist_connections

当成员被 `exclude_threshold` 剔除时，是否同时掐断它的已有连接。默认为 `false`（仅在选择时跳过）。

#### interrupt_exclude

命中任一条目的连接，在其出站被剔除时免于掐断。仅在 `interrupt_exist_connections` 开启时生效。

每个条目可含 `domain_suffix`、`package_name`、`process_name`、`process_path`、`port`、`rule_set` 条件，匹配语义与路由规则一致：条目内多条件需全部满足（AND），条目间任一满足即豁免（OR）。

```json
{
  "interrupt_exclude": [
    {
      "domain_suffix": ["bank.com"],
      "package_name": ["com.example.bank"]
    },
    {
      "port": [22]
    }
  ]
}
```

注意：空条目启动时直接报错；豁免只保"不断开"，被剔节点照样不接新连接。域名条件要求域名可见（经 DNS、Fake-IP 反查或嗅探）；无可见域名的连接永远不命中域名条件。`package_name` 仅 Android 有效，`process_name`/`process_path` 仅桌面有效。
