---
status: superseded by ADR-0012
---

# Agent ID represents a physical machine

An Agent ID represents the physical machine running Orbit rather than an OS or Agent installation. Deployment may provide an explicit `agent_id`, which is authoritative; otherwise the macOS Agent derives one by applying an Orbit-namespaced hash to the machine Platform UUID so the raw UUID is never exposed on the wire. The operator must keep an override stable across reinstallations and use one for virtual machines or environments whose local identifiers cannot provide the intended machine identity. MQTT connection settings and credentials are supplied as deployment configuration; Host Label remains display-only.
