# Orbit ASR observability

Core exposes the durable ASR queue at `GET /metrics`. The Prometheus service
must be attached to the external `leo_db_core` Docker network so it can scrape
`orbit-core:7620`; the endpoint is intentionally reachable only on that
internal network and is not published through the public reverse proxy.

The files in this directory are the Prometheus scrape configuration and the
Grafana dashboard provisioning used by the TX deployment.
