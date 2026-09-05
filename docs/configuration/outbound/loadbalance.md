### Structure

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

    You can ignore the JSON Array [] tag when the content is only one item

### Fields

#### strategy

Load Balancing Strategies.

* `round-robin` will distribute all requests among different proxy nodes within the strategy group.

* `consistent-hashing` will assign requests with the same `target address` to the same proxy node within the strategy group.

* `sticky-sessions`: requests with the same `source address` and `target address` will be directed to the same proxy node within the strategy group, with a cache expiration of specified ttl.

!!! note
    When the `target address` is a domain, it uses top-level domain matching.

#### outbounds

List of outbound tags to test.

#### providers

List of [Provider](/configuration/provider) tags to test.

#### exclude

Exclude regular expression to filter `providers` nodes.

#### include

Include regular expression to filter `providers` nodes.

#### url

The URL to test. `https://www.gstatic.com/generate_204` will be used if empty.

#### interval

The test interval. `3m` will be used if empty.

#### idle_timeout

The idle timeout. `30m` will be used if empty.

#### ttl

The time to live used for `sticky-sessions` strategy  timeout. `10m` will be used if empty.

#### use_all_providers

Whether to use all providers for testing. `false` will be used if empty.

#### exclude_threshold

Maximum number of dial failures per outbound within a 15-minute window before it is temporarily excluded from load balancing. Applies to all strategies (`round-robin`, `consistent-hashing`, `sticky-sessions`). Only real traffic dial failures are counted, except cancellations caused by the caller. URL test failures only affect availability and are not counted. Excluded outbounds are re-included when the next 15-minute window starts. Defaults to `0` (disabled).

#### interrupt_exist_connections

When a member is excluded by `exclude_threshold`, whether to also close its existing connections. `false` will be used if empty (only skip it in selection).

#### interrupt_exclude

Connections matching any entry are protected from interruption when their outbound is excluded. Only takes effect when `interrupt_exist_connections` is enabled.

Each entry may contain `domain_suffix`, `package_name`, `process_name`, `process_path`, `port` and `rule_set` conditions, using the same matching semantics as route rules. Conditions within one entry must all match (AND); entries are ORed: a connection is protected if any entry fully matches.

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

Notes: an empty entry is rejected at startup; protection only skips closing, the excluded outbound still takes no new connections. Domain conditions require the domain to be visible (via DNS, Fake-IP reverse mapping or sniffing); connections without a visible domain never match domain conditions. `package_name` only matches on Android, `process_name`/`process_path` only on desktop.
