---
status: accepted
---

# Core owns Node data routing

Nodes neither select nor observe which Agent produced their data. Core exclusively owns the Projection Route from canonical Observations to each Node's DeviceView, keeping Node protocol and firmware independent of Agent topology and preventing self-reported NodeState from becoming an authorization or data-selection mechanism. This supersedes ADR-0016.
