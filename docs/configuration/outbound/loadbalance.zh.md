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
  "exclude_threshold": 0
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
