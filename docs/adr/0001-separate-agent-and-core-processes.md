---
status: accepted
---

# Run Agent and Core as separate processes

The first usable release runs Orbit Agent and Orbit Core as separate processes that communicate through MQTT. This costs an additional process and identity, but exercises the real message, reconnection, and authorization boundaries from the first integration instead of hiding them behind an in-process substitute.
