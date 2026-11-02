# Security

The current host capability is limited to the typed `OpenCodexSession` action.
Authorized App requests are validated by Core before it resolves the target
Agent and publishes commands; Agents subscribe only to their own command topic.
The action accepts a lowercase UUID, constructs the fixed
`codex://threads/{session_id}` scheme locally, and never accepts a URL or shell
command from the network. Commands expire within 30 seconds and are deduplicated
in bounded process memory.

Core serves a single browser workbench. `/box` and management pages share the
existing password and persistent random session, stored in an HttpOnly,
SameSite=Strict cookie. Management APIs enforce the console's origin checks.
Device bearer tokens and `/api/v1` device cookies cannot authorize management
requests; management cookies cannot authorize device requests. Static assets
remain public so browsers can render the login form. The standalone browser
proxy has been removed. Workbench SSE streams contain watermarks only and
reconnect at least once a minute to revalidate their session; content requests
always pass through the normal authorization middleware.

The inbox stores local drafts and pending operations in IndexedDB, without
additional content encryption. Signing out clears that workbench cache after
confirmation when unsynced content exists. Revocation cannot erase content
already downloaded by a device. Never cache private API responses in a shared
proxy or service worker.

The full threat model, credential lifecycle, ACL examples, and local-confirmation
policy still need to be specified before destructive or privacy-sensitive host
capabilities are connected to a broker.

The current security baseline is recorded in [design.md](design.md#11-安全与隐私).
