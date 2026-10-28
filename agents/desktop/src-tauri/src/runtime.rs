use crate::{
    clock::Clock,
    codex,
    commands::{metadata, Executor},
    config::{Config, Secrets},
    usage,
    wire::{self, observation::Payload, AgentState, Observation, SourceStatus},
};
use prost::Message;
use rumqttc::{
    v5::{
        mqttbytes::{
            v5::{DisconnectReasonCode, Packet, PublishProperties, SubscribeReasonCode},
            QoS,
        },
        AsyncClient, Event, MqttOptions,
    },
    Transport,
};
use serde::Serialize;
use std::{
    collections::HashMap,
    io::BufReader,
    sync::{Arc, Mutex},
    time::Duration,
};
use tokio::{sync::mpsc, task::JoinSet};
use tokio_util::sync::CancellationToken;

#[derive(Clone, Default, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Status {
    pub running: bool,
    pub connected: bool,
    pub message: String,
    pub codex: SourceView,
    pub usage: SourceView,
    pub weekly_error: String,
    pub weekly_diagnostics: WeeklyDiagnostics,
    pub clock_error: String,
    pub last_ack: Option<i64>,
    pub inference: InferenceView,
}
// Retain the last failure after recovery so intermittent errors remain inspectable.
#[derive(Clone, Default, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WeeklyDiagnostics {
    pub last_attempt: Option<i64>,
    pub last_success: Option<i64>,
    pub last_failure: Option<i64>,
    pub duration_ms: u64,
    pub failure_duration_ms: u64,
    pub consecutive_failures: u32,
    pub next_attempt: Option<i64>,
    pub last_error: Option<codex::WeeklyError>,
}
fn record_weekly_attempt(
    status: &SharedStatus,
    result: &Result<wire::CodexWeeklyLimit, codex::WeeklyError>,
    attempted_at: i64,
    completed_at: i64,
    duration_ms: u64,
    delay: Duration,
) {
    update(status, |v| {
        let d = &mut v.weekly_diagnostics;
        d.last_attempt = Some(attempted_at);
        d.duration_ms = duration_ms;
        d.next_attempt = Some(completed_at + delay.as_secs() as i64);
        match result {
            Ok(value) => {
                d.last_success = value.observed_at.map(|t| t.seconds);
                d.consecutive_failures = 0;
                v.weekly_error.clear();
            }
            Err(error) => {
                d.last_failure = Some(completed_at);
                d.failure_duration_ms = duration_ms;
                d.consecutive_failures = d.consecutive_failures.saturating_add(1);
                d.last_error = Some(error.clone());
                v.weekly_error = error.to_string();
            }
        }
    });
}
#[derive(Clone, Default, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SourceView {
    pub last_success: Option<i64>,
    pub error: String,
    pub sessions: Option<u32>,
    pub running_sessions: Option<u32>,
}
#[derive(Clone, Default, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct InferenceView {
    pub status: String,
    pub connected: bool,
    pub device: String,
    pub queued: u32,
    pub outbox: u32,
    pub error: String,
    pub model_id: String,
    pub model_ready: bool,
    pub model_downloading: bool,
    pub model_downloaded: u64,
    pub model_total: u64,
    pub model_error: String,
}
pub type SharedStatus = Arc<Mutex<Status>>;
fn update(status: &SharedStatus, f: impl FnOnce(&mut Status)) {
    if let Ok(mut s) = status.lock() {
        f(&mut s)
    }
}
pub fn snapshot(status: &SharedStatus) -> Status {
    status.lock().map(|s| s.clone()).unwrap_or_default()
}

pub fn mqtt_options(c: &Config, s: &Secrets, client_id: String) -> Result<MqttOptions, String> {
    let u = url::Url::parse(&c.mqtt.url).map_err(|_| "MQTT 地址无效")?;
    let tls = u.scheme() != "mqtt";
    let mut o = MqttOptions::new(
        client_id,
        u.host_str().ok_or("MQTT 地址无效")?,
        u.port().unwrap_or(if tls { 8883 } else { 1883 }),
    );
    o.set_keep_alive(Duration::from_secs(30))
        .set_clean_start(true)
        .set_connection_timeout(8)
        .set_max_packet_size(Some(32768))
        .set_outgoing_inflight_upper_limit(16);
    if !c.mqtt.username.is_empty() || !s.mqtt.is_empty() {
        o.set_credentials(&c.mqtt.username, &s.mqtt);
    }
    if tls {
        let mut roots = rustls::RootCertStore::empty();
        if c.mqtt.ca_file.is_empty() {
            for cert in rustls_native_certs::load_native_certs().map_err(|_| "无法读取系统证书")?
            {
                let _ = roots.add(cert);
            }
        } else {
            let bytes = std::fs::read(&c.mqtt.ca_file).map_err(|_| "无法读取 CA 证书")?;
            for cert in rustls_pemfile::certs(&mut BufReader::new(&bytes[..])) {
                roots
                    .add(cert.map_err(|_| "CA 证书格式不正确")?)
                    .map_err(|_| "CA 证书无效")?;
            }
        }
        if roots.is_empty() {
            return Err("没有可用的 CA 证书".into());
        }
        let builder = rustls::ClientConfig::builder().with_root_certificates(roots);
        let config = if c.mqtt.cert_file.is_empty() {
            builder.with_no_client_auth()
        } else {
            let cert = std::fs::read(&c.mqtt.cert_file).map_err(|_| "无法读取客户端证书")?;
            let key = std::fs::read(&c.mqtt.key_file).map_err(|_| "无法读取客户端私钥")?;
            let certs = rustls_pemfile::certs(&mut BufReader::new(&cert[..]))
                .collect::<Result<Vec<_>, _>>()
                .map_err(|_| "客户端证书格式不正确")?;
            let key = rustls_pemfile::private_key(&mut BufReader::new(&key[..]))
                .map_err(|_| "客户端私钥格式不正确")?
                .ok_or("找不到客户端私钥")?;
            builder
                .with_client_auth_cert(certs, key)
                .map_err(|_| "客户端证书和私钥不匹配")?
        };
        o.set_transport(Transport::tls_with_config(config.into()));
    }
    Ok(o)
}
pub async fn test_connection(c: &Config, s: &Secrets) -> Result<String, String> {
    c.validate(s)?;
    let options = mqtt_options(c, s, format!("orbit-test-{}", uuid::Uuid::new_v4()))?;
    let (_client, mut events) = AsyncClient::new(options, 4);
    tokio::time::timeout(Duration::from_secs(10), async {
        loop {
            match events.poll().await {
                Ok(Event::Incoming(Packet::ConnAck(_))) => {
                    return Ok("MQTT 连接和身份验证成功".into())
                }
                Err(_) => return Err("无法连接 MQTT，请检查地址、凭据和证书".into()),
                _ => {}
            }
        }
    })
    .await
    .map_err(|_| "MQTT 连接超时".to_string())?
}
enum SourceEvent {
    Codex(Result<wire::CodexObservation, String>),
    Usage(Result<wire::UsageObservation, String>),
    Weekly(Result<wire::CodexWeeklyLimit, codex::WeeklyError>),
}
fn spawn_sources(
    c: &Config,
    s: &Secrets,
    clock: Arc<Clock>,
    tx: mpsc::Sender<SourceEvent>,
    workers: &mut JoinSet<()>,
    status: &SharedStatus,
) {
    if c.codex.enabled {
        let config = c.codex.clone();
        let out = tx.clone();
        let time = clock.clone();
        workers.spawn(async move {
            loop {
                let copy = config.clone();
                let now = time.now();
                let result = tokio::task::spawn_blocking(move || codex::collect(&copy, now))
                    .await
                    .unwrap_or_else(|_| Err("Codex 采集异常".into()));
                if out.send(SourceEvent::Codex(result)).await.is_err() {
                    break;
                }
                tokio::time::sleep(Duration::from_secs(config.interval)).await;
            }
        });
        if c.codex.rate_limits {
            let config = c.codex.clone();
            let out = tx.clone();
            let time = clock.clone();
            let status = status.clone();
            workers.spawn(async move {
                let mut retry = codex::WeeklyRetry::default();
                loop {
                    let attempted_at = time.now();
                    let started = std::time::Instant::now();
                    let result = codex::weekly(&config, attempted_at).await;
                    let delay = retry.delay(result.as_ref().err());
                    record_weekly_attempt(
                        &status,
                        &result,
                        attempted_at,
                        time.now(),
                        started.elapsed().as_millis() as u64,
                        delay,
                    );
                    if out.send(SourceEvent::Weekly(result)).await.is_err() {
                        break;
                    }
                    tokio::time::sleep(delay).await;
                }
            });
        }
    }
    if c.usage.enabled {
        let config = c.usage.clone();
        let password = s.usage.clone();
        workers.spawn(async move {
            let Ok(mut source) = usage::Source::new(config.clone(), password) else {
                return;
            };
            loop {
                let result = source.fetch(clock.now()).await;
                let delay = match &result {
                    Err(usage::Error::RateLimited(d)) => {
                        (*d).max(Duration::from_secs(config.interval))
                    }
                    _ => Duration::from_secs(config.interval),
                };
                if tx
                    .send(SourceEvent::Usage(result.map_err(|e| e.message())))
                    .await
                    .is_err()
                {
                    break;
                }
                tokio::time::sleep(delay).await;
            }
        });
    }
}
fn publish(
    client: &AsyncClient,
    topic: String,
    retained: bool,
    bytes: Vec<u8>,
    ttl: Option<u32>,
) -> Result<(), String> {
    client
        .try_publish_with_properties(
            topic,
            QoS::AtLeastOnce,
            retained,
            bytes,
            PublishProperties {
                content_type: Some("application/protobuf".into()),
                message_expiry_interval: ttl,
                ..Default::default()
            },
        )
        .map_err(|_| "发送队列暂时不可用".into())
}
fn facts(payload: &Payload) -> Vec<u8> {
    match payload {
        Payload::Codex(c) => {
            let mut c = c.clone();
            c.observed_at = None;
            for s in &mut c.sessions {
                s.updated_at = None
            }
            c.sessions.sort_by(|a, b| a.session_id.cmp(&b.session_id));
            c.encode_to_vec()
        }
        Payload::Usage(u) => {
            let mut u = u.clone();
            u.observed_at = None;
            u.encode_to_vec()
        }
    }
}
fn source_status(kind: i32, enabled: bool, v: &SourceView) -> SourceStatus {
    SourceStatus {
        observation_type: kind,
        enabled,
        health: if !enabled {
            0
        } else if !v.error.is_empty() {
            if v.last_success.is_some() {
                2
            } else {
                3
            }
        } else if v.last_success.is_some() {
            1
        } else {
            0
        },
        last_success_at: v.last_success.and_then(codex::ts),
        error_code: if v.error.is_empty() {
            ""
        } else {
            "source_unavailable"
        }
        .into(),
    }
}
struct Publisher {
    announced: bool,
    announcement_queued: bool,
    announcement_pkid: Option<u16>,
    epoch: String,
    state_facts: Vec<u8>,
    state_revision: u64,
    revisions: [u64; 2],
    sent: HashMap<usize, (Vec<u8>, i64)>,
}
impl Publisher {
    fn new() -> Self {
        Self {
            epoch: uuid::Uuid::new_v4().to_string(),
            state_facts: Vec::new(),
            announced: false,
            announcement_queued: false,
            announcement_pkid: None,
            state_revision: 0,
            revisions: [0, 0],
            sent: HashMap::new(),
        }
    }
    fn flush(
        &mut self,
        client: &AsyncClient,
        c: &Config,
        status: &Status,
        latest: &[Option<Payload>; 2],
        now: i64,
    ) -> Result<(), String> {
        if !self.announced && self.announcement_queued {
            return Ok(());
        }
        let mut state = AgentState {
            metadata: Some(metadata(&c.agent_id, self.state_revision, now, None)),
            agent_id: c.agent_id.clone(),
            agent_epoch: self.epoch.clone(),
            agent_version: env!("CARGO_PKG_VERSION").into(),
            host_label: c.host_label.clone(),
            sources: vec![
                source_status(1, c.usage.enabled, &status.usage),
                source_status(2, c.codex.enabled, &status.codex),
            ],
        };
        let mut state_business = state.clone();
        state_business.metadata = None;
        for source in &mut state_business.sources {
            source.last_success_at = None;
        }
        let state_facts = state_business.encode_to_vec();
        if state_facts != self.state_facts {
            self.state_revision += 1;
            state.metadata = Some(metadata(&c.agent_id, self.state_revision, now, None));
            publish(
                client,
                format!("orbit/v1/agents/{}/state", c.agent_id),
                true,
                state.encode_to_vec(),
                None,
            )?;
            self.state_facts = state_facts;
        }
        if !self.announced {
            self.announcement_queued = true;
            return Ok(());
        }
        for (index, value) in latest.iter().enumerate() {
            let Some(payload) = value else { continue };
            let (name, ttl, interval, observed) = match payload {
                Payload::Usage(p) => (
                    "usage",
                    c.usage.ttl,
                    c.usage.interval,
                    p.observed_at.as_ref(),
                ),
                Payload::Codex(p) => (
                    "codex",
                    c.codex.ttl,
                    c.codex.interval,
                    p.observed_at.as_ref(),
                ),
            };
            let Some(observed) = observed else { continue };
            if observed.seconds + ttl as i64 <= now {
                continue;
            }
            let facts = facts(payload);
            if self.sent.get(&index).is_some_and(|(old, expiry)| {
                old == &facts
                    && (observed.seconds + ttl as i64 == *expiry
                        || now + (interval as i64) < *expiry)
            }) {
                continue;
            }
            self.revisions[index] += 1;
            let expires = observed.seconds + ttl as i64;
            let observation = Observation {
                metadata: Some(metadata(
                    &c.agent_id,
                    self.revisions[index],
                    now,
                    Some(expires - now),
                )),
                agent_epoch: self.epoch.clone(),
                payload: Some(payload.clone()),
            };
            publish(
                client,
                format!("orbit/v1/agents/{}/observations/{name}", c.agent_id),
                false,
                observation.encode_to_vec(),
                Some((expires - now) as u32),
            )?;
            self.sent.insert(index, (facts, expires));
        }
        Ok(())
    }
}
fn record_source(
    event: SourceEvent,
    latest: &mut [Option<Payload>; 2],
    weekly: &mut Option<wire::CodexWeeklyLimit>,
    status: &SharedStatus,
    now: i64,
) {
    match event {
        SourceEvent::Codex(result) => match result {
            Ok(mut value) => {
                value.weekly_limit =
                    weekly.filter(|w| w.resets_at.as_ref().is_some_and(|t| t.seconds > now));
                update(status, |v| {
                    v.codex.last_success = value.observed_at.as_ref().map(|t| t.seconds);
                    v.codex.error.clear();
                    v.codex.sessions = Some(value.total_count);
                    v.codex.running_sessions = Some(value.running_count);
                });
                latest[1] = Some(Payload::Codex(value));
            }
            Err(e) => update(status, |v| v.codex.error = e),
        },
        SourceEvent::Usage(result) => match result {
            Ok(value) => {
                update(status, |v| {
                    v.usage.last_success = value.observed_at.as_ref().map(|t| t.seconds);
                    v.usage.error.clear();
                });
                latest[0] = Some(Payload::Usage(value));
            }
            Err(e) => update(status, |v| v.usage.error = e),
        },
        SourceEvent::Weekly(result) => {
            if let Ok(value) = result {
                *weekly = Some(value);
                // Publish recovered quota without waiting for the next session scan.
                if let Some(Payload::Codex(observation)) = &mut latest[1] {
                    observation.weekly_limit =
                        weekly.filter(|w| w.resets_at.is_some_and(|t| t.seconds > now));
                }
            }
        }
    }
}

pub async fn run(c: Config, s: Secrets, status: SharedStatus, cancel: CancellationToken) {
    update(&status, |v| {
        v.running = true;
        v.weekly_error.clear();
        v.weekly_diagnostics = WeeklyDiagnostics::default();
        v.message = "正在连接".into();
        v.connected = false;
    });
    let clock = Arc::new(Clock::default());
    let mut workers = JoinSet::new();
    let time = clock.clone();
    let server = c.ntp_server.clone();
    let ntp_timeout = c.ntp_timeout;
    let ntp_interval = c.ntp_interval;
    let view = status.clone();
    workers.spawn(async move {
        loop {
            let result = time.sync(&server, ntp_timeout).await;
            update(&view, |v| v.clock_error = result.err().unwrap_or_default());
            tokio::time::sleep(Duration::from_secs(ntp_interval)).await;
        }
    });
    // Bound initial correction before timestamps are produced; NTP failure falls back to system time.
    tokio::select! {_ = cancel.cancelled()=>{},_ = tokio::time::sleep(Duration::from_secs(ntp_timeout))=>{}}
    let (tx, mut rx) = mpsc::channel(16);
    spawn_sources(&c, &s, clock.clone(), tx, &mut workers, &status);
    let mut latest = [None, None];
    let mut weekly = None;
    let mut executor = Executor::default();
    let mut backoff = 1u64;
    'reconnect: loop {
        if cancel.is_cancelled() {
            break;
        }
        let options = match mqtt_options(&c, &s, format!("orbit-agent-{}", c.agent_id)) {
            Ok(o) => o,
            Err(e) => {
                update(&status, |v| v.message = e);
                break;
            }
        };
        let (client, mut eventloop) = AsyncClient::new(options, 128);
        let mut publisher = Publisher::new();
        let mut online = false;
        let mut catalog_sent = false;
        let mut tick = tokio::time::interval(Duration::from_secs(5));
        tick.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
        loop {
            let mut dirty = false;
            // poll() must finish the CONNECT handshake before other branches can cancel it.
            tokio::select! {
                _ = cancel.cancelled() => break 'reconnect,
                event = rx.recv(), if online => {
                    if let Some(event) = event {
                        record_source(event, &mut latest, &mut weekly, &status, clock.now());
                        dirty = true;
                    }
                },
                _ = tick.tick(), if online => dirty = true,
                event = eventloop.poll() => match event {
                    Ok(Event::Incoming(Packet::ConnAck(_))) => {
                        online = true;
                        backoff = 1;
                        update(&status, |v| {v.connected = true; v.message = "已连接 MQTT".into()});
                        let mut filter = rumqttc::v5::mqttbytes::v5::Filter::new(format!("orbit/v1/agents/{}/commands", c.agent_id), QoS::AtLeastOnce);
                        // Preserve the publisher's RETAIN flag so live retained commands are rejected too.
                        filter.preserve_retain = true;
                        if client.try_subscribe_many([filter]).is_err() {
                            break;
                        }
                        dirty = true;
                    },
                    Ok(Event::Outgoing(rumqttc::Outgoing::Publish(pkid))) => {
                        if !publisher.announced && publisher.announcement_pkid.is_none() {
                            publisher.announcement_pkid = Some(pkid);
                        }
                    },
                    Ok(Event::Incoming(Packet::SubAck(ack))) => {
                        if ack.return_codes.iter().any(|code| !matches!(code, SubscribeReasonCode::Success(_))) {
                            update(&status, |v| v.message = "没有订阅指令的权限，请检查 MQTT ACL".into());
                            break 'reconnect;
                        }
                    },
                    Ok(Event::Incoming(Packet::PubAck(ack))) => {
                        use rumqttc::v5::mqttbytes::v5::PubAckReason;
                        if matches!(ack.reason, PubAckReason::Success | PubAckReason::NoMatchingSubscribers) {
                            update(&status, |v| v.last_ack = Some(clock.now()));
                            if publisher.announcement_pkid == Some(ack.pkid) {
                                publisher.announced = true;
                                publisher.announcement_pkid = None;
                                if !catalog_sent {
                                    let catalog = crate::shortcuts::catalog(&c).encode_to_vec();
                                    if publish(&client, format!("orbit/v1/agents/{}/shortcuts",c.agent_id), true, catalog, None).is_err(){break;}
                                    catalog_sent = true;
                                }
                                dirty = true;
                            }
                        } else {
                            update(&status, |v| v.message = "MQTT 拒绝发布，请检查发布权限".into());
                            break 'reconnect;
                        }
                    },
                    Ok(Event::Incoming(Packet::Publish(p))) => {
                        let expected_topic = format!("orbit/v1/agents/{}/commands", c.agent_id);
                        if !p.retain && !p.payload.is_empty() && p.payload.len() <= 32768 && p.topic.as_ref() == expected_topic.as_bytes() {
                            if let Ok(command) = wire::Command::decode(p.payload) {
                                let result = tokio::select! {
                                    _ = cancel.cancelled() => break 'reconnect,
                                    result = executor.execute_config(command, &c, clock.now_millis()) => result
                                };
                                if publish(&client, format!("orbit/v1/agents/{}/results", c.agent_id), false, result.encode_to_vec(), Some(30)).is_err() {
                                    break;
                                }
                            }
                        }
                    },
                    Ok(Event::Incoming(Packet::Disconnect(d))) => {
                        if d.reason_code == DisconnectReasonCode::SessionTakenOver {
                            update(&status, |v| v.message = "设备标识被另一实例使用，请先停止旧 Agent".into());
                            break 'reconnect;
                        }
                        break;
                    },
                    Err(rumqttc::v5::ConnectionError::MqttState(rumqttc::v5::StateError::ServerDisconnect {reason_code: DisconnectReasonCode::SessionTakenOver, ..})) => {
                        update(&status, |v| v.message = "设备标识被另一实例使用，请先停止旧 Agent".into());
                        break 'reconnect;
                    },
                    Err(rumqttc::v5::ConnectionError::MqttState(rumqttc::v5::StateError::SubFail {..} | rumqttc::v5::StateError::PubAckFail {..})) => {
                        update(&status, |v| v.message = "MQTT 拒绝访问，请检查发布和订阅权限".into());
                        break 'reconnect;
                    },
                    Err(_) => break,
                    _ => {}
                }
            }
            if online
                && dirty
                && publisher
                    .flush(&client, &c, &snapshot(&status), &latest, clock.now())
                    .is_err()
            {
                break;
            }
        }
        update(&status, |v| {
            v.connected = false;
            v.message = if online {
                "连接中断，正在重试"
            } else {
                "无法连接 MQTT，请检查服务设置；正在重试"
            }
            .into()
        });
        tokio::select! {_=cancel.cancelled()=>break,_=tokio::time::sleep(Duration::from_secs(backoff))=>{}}
        backoff = (backoff * 2).min(30);
    }
    workers.abort_all();
    while workers.join_next().await.is_some() {}
    update(&status, |v| {
        v.running = false;
        v.connected = false;
        if cancel.is_cancelled() {
            v.message = "已暂停".into()
        }
    });
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn weekly_failure_preserves_stale_values_until_reset_and_recovers_immediately() {
        let status = Arc::new(Mutex::new(Status::default()));
        let mut latest = [None, None];
        let mut weekly = None;
        let quota = wire::CodexWeeklyLimit {
            remaining_percent: 42.,
            resets_at: codex::ts(1000),
            observed_at: codex::ts(100),
            fresh_until: codex::ts(280),
        };
        record_source(
            SourceEvent::Weekly(Ok(quota)),
            &mut latest,
            &mut weekly,
            &status,
            100,
        );
        record_source(
            SourceEvent::Weekly(Err(codex::WeeklyError::new("request", "timeout"))),
            &mut latest,
            &mut weekly,
            &status,
            400,
        );
        let observation = |now| wire::CodexObservation {
            observed_at: codex::ts(now),
            ..Default::default()
        };
        record_source(
            SourceEvent::Codex(Ok(observation(400))),
            &mut latest,
            &mut weekly,
            &status,
            400,
        );
        let Some(Payload::Codex(value)) = &latest[1] else {
            panic!("missing codex observation")
        };
        assert_eq!(value.weekly_limit, Some(quota)); // No renewal of original timestamps.
        record_source(
            SourceEvent::Codex(Ok(observation(1000))),
            &mut latest,
            &mut weekly,
            &status,
            1000,
        );
        let Some(Payload::Codex(value)) = &latest[1] else {
            panic!("missing codex observation")
        };
        assert!(value.weekly_limit.is_none());
        let recovered = wire::CodexWeeklyLimit {
            remaining_percent: 99.,
            resets_at: codex::ts(2000),
            observed_at: codex::ts(1001),
            fresh_until: codex::ts(1181),
        };
        record_source(
            SourceEvent::Weekly(Ok(recovered)),
            &mut latest,
            &mut weekly,
            &status,
            1001,
        );
        let Some(Payload::Codex(value)) = &latest[1] else {
            panic!("missing codex observation")
        };
        assert_eq!(value.weekly_limit, Some(recovered));
        assert_eq!(value.observed_at.unwrap().seconds, 1000); // Do not renew the session scan.
    }

    #[test]
    fn weekly_diagnostics_survive_recovery_without_renewing_last_success_on_failure() {
        let status = Arc::new(Mutex::new(Status::default()));
        let quota = wire::CodexWeeklyLimit {
            observed_at: codex::ts(100),
            ..Default::default()
        };
        record_weekly_attempt(&status, &Ok(quota), 100, 102, 2000, Duration::from_secs(60));
        let failure = Err(codex::WeeklyError::new("request", "timeout"));
        record_weekly_attempt(&status, &failure, 162, 177, 15000, Duration::from_secs(5));
        let current = snapshot(&status);
        assert_eq!(current.weekly_diagnostics.last_success, Some(100));
        assert_eq!(current.weekly_diagnostics.last_failure, Some(177));
        assert_eq!(current.weekly_diagnostics.next_attempt, Some(182));
        assert_eq!(current.weekly_diagnostics.consecutive_failures, 1);
        assert!(!current.weekly_error.is_empty());
        record_weekly_attempt(
            &status,
            &Ok(wire::CodexWeeklyLimit {
                observed_at: codex::ts(182),
                ..quota
            }),
            182,
            184,
            2000,
            Duration::from_secs(60),
        );
        let current = snapshot(&status);
        assert!(current.weekly_error.is_empty());
        assert_eq!(current.weekly_diagnostics.consecutive_failures, 0);
        assert_eq!(current.weekly_diagnostics.last_success, Some(182));
        assert_eq!(current.weekly_diagnostics.last_failure, Some(177));
        assert_eq!(current.weekly_diagnostics.failure_duration_ms, 15000);
        assert_eq!(
            current.weekly_diagnostics.last_error.unwrap().code,
            "timeout"
        );
    }

    #[test]
    fn facts_ignore_bookkeeping() {
        let a = wire::CodexObservation {
            observed_at: codex::ts(1),
            ..Default::default()
        };
        let mut b = a.clone();
        b.observed_at = codex::ts(2);
        assert_eq!(facts(&Payload::Codex(a)), facts(&Payload::Codex(b)));
    }
    #[test]
    fn failure_retains_last_success() {
        let v = SourceView {
            last_success: Some(1),
            error: "oops".into(),
            ..Default::default()
        };
        let s = source_status(2, true, &v);
        assert_eq!(s.health, 2);
        assert_eq!(s.last_success_at.unwrap().seconds, 1);
    }
}
