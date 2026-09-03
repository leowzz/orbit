---
status: accepted
---

# Use ephemeral idempotent V1 commands

V1 Commands are short-lived and may be lost or repeated across process restarts, so V1 admits only idempotent or coalescible Capabilities and uses bounded in-memory duplicate suppression. Durable CommandStore recovery and multi-stage execution states are deferred until a non-idempotent Capability creates a real need.
