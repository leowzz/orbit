---
status: superseded by ADR-0014
---

# Core owns Node registration

Core is authoritative for the registration that binds a `node_id` to its permitted Series, Model, Variant, and target Agent. A Node reports its observed hardware and firmware in a Hello message, but a mismatch quarantines the Node instead of mutating the registration from self-reported data.
