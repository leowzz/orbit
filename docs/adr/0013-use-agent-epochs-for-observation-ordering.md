---
status: accepted
---

# Use Agent Epochs for Observation ordering

An Agent generates a random `agent_epoch` on every process start and resets each Observation type's in-memory revision to one. It publishes retained AgentState containing the new Epoch before starting Sources, and Core accepts Observations only from that Agent's current Epoch. This avoids durable counters while preventing delayed messages from an older Agent process from replacing newer state.
