---
status: accepted
---

# Agent owns observation acquisition

All data acquisition runs behind Sources hosted by an Agent, including integrations that call remote APIs or subscribe to events. A Source is not a Command Capability or an independently authenticated MQTT participant: its Agent manages the Source lifecycle, wraps structured Observations, and publishes them under that Agent's identity. Each Agent admits at most one Source for each Observation type, keeping Core dependent on one observation boundary and avoiding a second producer lifecycle and ACL model.
