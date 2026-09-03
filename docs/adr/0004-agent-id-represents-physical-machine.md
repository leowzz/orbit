---
status: accepted
---

# Agent ID represents a physical machine

An Agent ID represents the physical machine running Orbit rather than an OS or Agent installation, so reinstalling or dual-booting keeps the identity while replacing the motherboard creates a new one. V1 supports macOS Agents and uses the platform UUID as the machine identity source; virtual machines require a persisted unique override because cloned platform identifiers cannot safely provide this identity.
