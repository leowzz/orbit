---
status: accepted
---

# Core processes only new Observations

V1 does not recover Core canonical state after a restart. Each Core process creates a new `core_epoch`, starts with empty state, and projects only Observations received after that process starts; it does not request Agent snapshots or consume old retained DeviceViews as input. DeviceViews carry the Core Epoch so Nodes can accept revisions restarted from one without confusing them with the previous process.
