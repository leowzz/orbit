---
status: accepted
---

# Retain participant state until tombstone

AgentState, CoreState, NodeState, and Presence have no MQTT Message Expiry and remain retained until replaced or explicitly cleared with a tombstone. Without application heartbeats, expiring unchanged participant state would make a still-connected participant undiscoverable; Orbit accepts the operational requirement to revoke credentials and clear retained Topics when removing a participant. DeviceView remains time-bounded by `fresh_until`, `retain_until`, and MQTT Message Expiry.
