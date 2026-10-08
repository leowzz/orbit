---
status: accepted
---

# App Nodes and durable inbox operations over HTTP

Personal items must survive Core restarts and device disconnection. MQTT retained
views and expiring host commands cannot provide a durable message history.

Core now serves an optional authenticated JSON/SSE API and a separate SQLite WAL
database in the same process. Agent/OLED/Web MQTT protocols remain unchanged.
`overview-app` routes are provisioned by the operator and read over HTTP; they
cannot be registered through MQTT NodeState. An App without a route can still use
the shared inbox. No host actions are exposed in the App until result feedback is
implemented. This extends ADR-0010 for durable user content and preserves ADR-0008,
ADR-0015 and ADR-0018 for observations and short-lived commands.

Use one small `inbox` package for transactions/history/attachments and `appapi` for
HTTP, rather than separate frameworks for devices, storage, sync and events.
Devices are operator-managed in Core configuration with independent SHA-256 token
digests; revocation/removal requires a restart. The API starts before MQTT connects.
HTTPS terminates at a trusted reverse proxy; only that proxy exposes the API.

Each operation atomically updates its item, immutable change and durable receipt.
Receipts are checked before revisions and are retained indefinitely. Each item has
an independent revision; changes have a persistent sequence and dataset generation.
Snapshot pages reconstruct each item's latest change at a fixed high-water mark,
so concurrent edits/deletes cannot move items out of a partially downloaded snapshot.
SSE sends watermarks immediately and every 15 seconds, as hints only. History and
tombstones are retained in this version; restoration requires explicitly rotating
the dataset generation. No physical foreign keys are added.

The shared Flutter App uses ChangeNotifier and sqflite (FFI on Windows), secure
credential storage and image_picker. Riverpod/Drift were suggestions, not required
contracts; these simpler choices avoid a generator and another state layer. The
cache and applied cursor commit together; full snapshot replacement is atomic.
The outbox is separate from authoritative items. One outstanding operation per
item keeps retry ordering explicit; further edits wait for acknowledgement or
conflict resolution. Conflicts preserve the draft for copying/re-editing.

Image uploads are bounded (10 MiB, 20 million pixels), decoded before acceptance,
and stored as authenticated files with JPEG thumbnails. Unreferenced uploads older
than 24 hours are reclaimed; upload completion precedes item creation. Local
thumbnail/original caches are on demand. macOS/Windows share sources, with platform
packaging and plugin verification tracked separately in the handoff.

The initial scope excludes background push, accounts, IM, CRDTs, multi-Core writers,
host action UI, and MQTT/Protobuf changes.
