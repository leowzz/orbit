---
status: accepted
---

# Use EMQX Cloud Serverless

V1 uses EMQX Cloud Serverless as its public MQTT 5 broker, with a distinct username and password plus least-privilege Topic ACLs for each Core, Agent, and Node. MQTT transport defaults to TLS but exposes an explicit protocol switch for alternate brokers that permit plaintext MQTT; transport choice does not change features or message handling, and clients never downgrade automatically after a TLS failure.
