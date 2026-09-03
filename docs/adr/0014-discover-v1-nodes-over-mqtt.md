---
status: accepted
---

# Discover V1 Nodes over MQTT

V1 has no Node Registration store or pre-registration workflow. Nodes and Core communicate through MQTT, and Core discovers a Node from protocol messages authenticated by that Node's Broker credentials. The exact self-description and Agent binding fields remain part of the MQTT protocol design rather than an external registration database or configuration file.
