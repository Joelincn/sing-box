---
icon: material/new-box
---

# Group

### Structure

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

### Fields

#### strategy

DNS query strategy. Available values:

- `concurrent` (default): Query all servers concurrently and return the fastest response.
- `round_robin`: Query only one server per request in round-robin order, no fallback. If the selected server fails, the error is returned immediately.

#### exclude_threshold

Maximum number of failures per server within a 15-minute window before it is temporarily excluded from the group. Applies to both `concurrent` and `round_robin` strategies. Excluded servers are re-included when the next 15-minute window starts. Defaults to `0` (disabled).

#### servers

==Required==

List of DNS server tags to include in this group.

Restrictions:

- A group cannot contain another group.
- A group cannot contain a `fakeip` server.

When queried, the behavior depends on the `strategy` field:

- **concurrent** (default): All servers in the group are queried concurrently, and the first successful response is returned.
- **round_robin**: Only the selected server is queried per request in round-robin order, no fallback to other servers. If it fails, the error is returned immediately.
