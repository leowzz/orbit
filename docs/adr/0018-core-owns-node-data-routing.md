---
status: accepted
---

# Core owns Node data routing

Nodes neither select nor observe which Agent produced their data. Core exclusively owns explicit, `node_id`-keyed Projection Routes from canonical Observations to each Node's DeviceView; V1 selects at most one Agent for each Observation type and never chooses or aggregates Agents implicitly. This keeps Node protocol and firmware independent of Agent topology and prevents self-reported NodeState from becoming an authorization or data-selection mechanism. This supersedes ADR-0016.
