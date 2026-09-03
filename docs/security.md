# Security

The current host capability is limited to the typed `OpenCodexSession` action.
Web Nodes publish only their own Intent topic; Core alone resolves the target
Agent and publishes commands; Agents subscribe only to their own command topic.
The action accepts a lowercase UUID, constructs the fixed
`codex://threads/{session_id}` scheme locally, and never accepts a URL or shell
command from the network. Commands expire within 30 seconds and are deduplicated
in bounded process memory.

The full threat model, credential lifecycle, ACL examples, and local-confirmation
policy still need to be specified before destructive or privacy-sensitive host
capabilities are connected to a broker.

The current security baseline is recorded in [design.md](design.md#11-安全与隐私).
