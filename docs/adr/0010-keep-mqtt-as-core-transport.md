---
status: accepted
---

# Keep MQTT as the core transport

MQTT is Orbit V1's only runtime infrastructure and its message backbone for Observations, AgentState, DeviceViews, Intents, Commands, and Results. Every Source is hosted by an Agent, and only the Agent publishes its Observations; there is no separate Backend Source MQTT identity. V1 introduces neither PostgreSQL nor Redis: Core loads read-only registration and policy from YAML and keeps canonical state in memory until a concrete persistence requirement justifies another dependency.
