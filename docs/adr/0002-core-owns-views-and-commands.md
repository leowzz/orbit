---
status: accepted
---

# Core owns views and commands

Only Orbit Core may construct and publish DeviceView messages or issue Commands. Agents publish Observations and Command Results only to Core, while Nodes and other clients submit Intents and receive requester-scoped Command Feedback from Core; this keeps privacy projection, command authorization, and result disclosure at one boundary and supersedes the earlier handoff wording that allowed an Agent to publish a view directly.
