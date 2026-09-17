## When to use

To answer only some networks on some routes, such as `/ops` from a VPN. Apps on `gorbital.Main` with the operations API set `OPS_ALLOWED_IPS` instead. Install [New](#New) after `httpx.TrustedProxies`, so it sees the client's address rather than the load balancer's.

- [Security layers](../guides/security-layers.md#ip-filter): rules, IPv6, and configuration.
- [Ops API](../guides/ops-api.md#restricting-ops-to-your-network): `OPS_ALLOWED_IPS`.
