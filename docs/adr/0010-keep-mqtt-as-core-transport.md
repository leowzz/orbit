---
status: accepted
---

# Keep MQTT as the core transport

MQTT is Orbit's required message backbone for Observations, DeviceViews, Intents, Commands, and Results. Core may connect to PostgreSQL for durable registration and policy or to Redis through optional adapters, but Redis is not an Orbit event bus and neither data store replaces MQTT participant communication; PostgreSQL relationships are enforced by application transactions without physical foreign keys.
