---
status: accepted
---

# Discover V1 Nodes over MQTT

V1 has no Node Registration store or pre-registration workflow. A Node publishes retained NodeState containing a boot-scoped `node_epoch`, its `node_id`, Series, Model, Variant, and firmware version; it does not declare an upstream Agent. Core subscribes to `orbit/#` and discovers the Node from that message. Broker credentials and ACL establish membership and Topic ownership, while Core validates Topic/payload consistency and supports only known product identities.
