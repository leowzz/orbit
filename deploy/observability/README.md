# Orbit ASR observability

## Runtime

Core serves `GET /metrics` on an independent, opt-in listener:

```yaml
metrics:
  listen: 0.0.0.0:7623
```

Omitting `metrics.listen` disables it. The console / App API do not expose
metrics. Do not publish port 7623 to the Internet. Prometheus joins the external
Docker network `leo_db_core` and scrapes `orbit-core:7623` every 15 seconds.

`compose.override.yml` is an additive example for the existing monitoring stack.
Copy the Grafana provider and JSON dashboard into its `grafana/provisioning/dashboards/`
directory, and merge the `orbit-core` job from `prometheus.yml` into its scrape
configuration (preserve unrelated jobs). The dashboard uses datasource UID
`prometheus`; change it if the installation uses another UID.

Validate with `promtool check config` and `promtool check metrics`. Recreate only
the services needing network / volume changes; subsequent Prometheus changes
can reload via SIGHUP, and Grafana polls provisioned dashboard files automatically.
Update Core before upgrading workers, because older Core versions reject unknown
WorkerState JSON fields.

## Metric semantics

| Metric | Type | Meaning |
| --- | --- | --- |
| `orbit_audio_jobs{status}` | gauge | Retained Core pending/completed/failed jobs. Pending includes dispatched jobs awaiting results. |
| `orbit_audio_pending_oldest_age_seconds` | gauge | Time since Core accepted its oldest pending job; 0 when none. |
| `orbit_audio_worker_jobs{agent_id,stage}` | gauge | Local queued / processing / outbox counts; only emitted with fresh live queue telemetry. |
| `orbit_audio_worker_queue_capacity` | gauge | Capacity including queued, processing and unacknowledged results. |
| `orbit_audio_worker_up` | gauge | Non-retained heartbeat received less than 60 seconds ago and status is not offline. |
| `orbit_audio_worker_queue_reported` | gauge | 1 if queue data is fresh and available; otherwise 0 (not proof of an empty queue). |
| `orbit_audio_worker_info` | gauge | Last identity/model/device/status plus connection state. |
| `orbit_audio_worker_last_received_timestamp_seconds` | gauge | Last Core receipt time, independent of worker clock skew. |
| `orbit_audio_received_total` | counter | Unique segments accepted since instrumentation began. |
| `orbit_audio_results_total{agent_id,status}` | counter | Unique results accepted since instrumentation began. |
| `orbit_audio_processed_audio_seconds_total` | counter | Audio duration covered by those results. |
| `orbit_audio_processing_seconds_total` | counter | Worker-reported processing time for those results; final attempt only. |
| `orbit_audio_segments_retained`, `orbit_audio_retention_limit` | gauge | Retained segment count and configured cap. |
| `orbit_audio_duration_seconds_retained`, `orbit_audio_speech_seconds_retained` | gauge | Audio/speech duration within the retention window. |
| `orbit_audio_job_attempts` | gauge | Dispatch attempts summed over retained jobs; not a monotonic counter. |

Counters are committed in the same SQLite transaction as business data, do not
double-count duplicate results, and survive retention and restarts. They start
at deployment, without fabricating historical events. The first two scrapes of
a series are required for a rate; idle workers have no average processing time.
Gauges for retained records must not be used to calculate consumption rates.
Core pending and Worker local queues overlap and must not be added together.
No transcript, audio, job ID, session ID, credentials or error text becomes a label.

## TX deployment (2026-10-31, Asia/Shanghai)

- Initial metrics image: `orbit-core:asr-metrics-20261031-r2` (locally built on TX).
  A concurrent audio-review deployment subsequently updated Core to
  `orbit-core:audio-review-c578856-linux`; the final live validation confirmed
  that this newer image also contains the metrics listener and counters.
- Metrics: private `orbit-core:7623/metrics`; no host port publication.
- Prometheus: `/srv/10-ops/observability/prometheus/prometheus.yml`, job `orbit-core`.
- Grafana: `https://grafana.wleo.cn/d/orbit-asr`, folder Orbit, 19 panels.
- Dashboard files: `/srv/10-ops/observability/grafana/provisioning/dashboards/`.
- Core backup: `/srv/orbit-deploy/backups/asr-metrics-20261031/`.
  `core-online.sqlite` is a verified SQLite online backup; the earlier raw
  `core.sqlite` copy is not a complete WAL backup and should not be used to restore.
- Monitoring configuration backup: `/srv/10-ops/observability/backups/asr-metrics-20261031/`.
- Build input and verification script: `/srv/orbit-deploy/cp/asr-metrics-20261031/`.

To disable monitoring without reverting subsequent application work, remove the
Prometheus scrape job and `metrics.listen`, then restart only Core. For a full
rollback to the original snapshot, first verify that reverting the concurrent
audio-review deployment is intended; the old snapshot predates that work.
Rollback the worker update first (old Core rejects the new optional queue field),
then restore the old Core compose and configuration from the backup and recreate
only `orbit-core`. For monitoring rollback, restore its compose and Prometheus
config, then recreate Prometheus/Grafana. Do not restore SQLite just to roll back
code: the extra counters table is additive and old code ignores it. A database
restore would discard subsequent recordings/results.

Validation: focused Go audio/config tests and vet, Rust queue/heartbeat tests,
25 Python worker tests, Prometheus config/exposition validation, live scrape `up=1`,
and every Grafana panel query via the provisioned datasource. The full Go suite
has unrelated Windows SQLite URI failures in `internal/sources/codex`.

The local Windows native Worker was rebuilt with queue telemetry. Its existing
SQLite queue was backed up online and retained; the current and normal user
configuration paths were synchronized after resolving MSIX app-data redirection.
The native queue was checked through both paths and refers to the same data.

The global message production/consumption panel compares per-minute rates of
`orbit_audio_received_total` and `orbit_audio_results_total` (all workers,
including completed and failed results). It deliberately ignores the Worker
filter so both sides describe the same global queue. Duplicate deliveries do
not count. The legend includes latest, mean and maximum values.
