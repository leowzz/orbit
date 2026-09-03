---
status: superseded by ADR-0008
---

# Make at most one command attempt

For each `command_id`, an Agent starts at most one Capability invocation and never automatically replays it after a crash. If the invocation began but its effect cannot be determined, the Command terminates as `UNKNOWN_OUTCOME`; this favors avoiding duplicate effects over automatic recovery and deliberately does not claim exactly-once execution.
