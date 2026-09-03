---
status: accepted
---

# Agent ID identifies an Agent instance

An Agent ID identifies a running Agent instance, not the physical host itself; Orbit normally runs one Agent per machine but allows multiple instances when each has an explicit distinct ID. Deployment may provide an authoritative ID, otherwise macOS derives `agt_` plus the first 128 bits of a SHA-256 hash over the `orbit.agent.v1` namespace and normalized Platform UUID. MQTT username and secret are configured independently, while Broker ACL binds those credentials to one Agent ID and MQTT Client ID is derived from that ID.
