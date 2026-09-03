---
status: superseded by ADR-0018
---

# Let V1 Nodes select their Agent

A V1 Node may declare any Agent in its Orbit Network as its single `target_agent_id`, and Core accepts that declaration without a separate binding registry. This is acceptable while Nodes receive only privacy-filtered DeviceViews and expose no product Intent; the decision must be revisited before a Node can cause real Commands, because view selection does not grant command authority.
