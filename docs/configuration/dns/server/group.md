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

        "servers": [
          "dns-a",
          "dns-b"
        ]
      }
    ]
  }
}
```

### Fields

#### strategy

DNS query strategy. Available values:

- `concurrent` (default): Query all servers concurrently and return the fastest response.
- `round_robin`: Query servers in round-robin order with automatic failover.

#### servers

==Required==

List of DNS server tags to include in this group.

Restrictions:

- A group cannot contain another group.
- A group cannot contain a `fakeip` server.

When queried, the behavior depends on the `strategy` field:

- **concurrent** (default): All servers in the group are queried concurrently, and the first successful response is returned.
- **round_robin**: Servers are queried in round-robin order. If a server fails, the next server is tried automatically.
