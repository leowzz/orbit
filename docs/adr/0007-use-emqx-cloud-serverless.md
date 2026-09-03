---
status: accepted
---

# Use EMQX Cloud Serverless

V1 uses one dedicated EMQX Cloud Serverless deployment as the boundary of one Orbit Network, with a distinct username and password plus least-privilege Topic ACLs for each Core, Agent, and Node. The protocol adds no tenant or network identifier. MQTT transport defaults to TLS but exposes an explicit protocol switch for alternate brokers that permit plaintext MQTT; transport choice does not change features or message handling, and clients never downgrade automatically after a TLS failure.
