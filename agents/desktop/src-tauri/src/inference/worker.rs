use super::{
    assets,
    engine::{EngineError, SpeechApi, SpeechModel},
    opus::Decoder,
    protocol::{decode_job, Job, ResultMessage, TextSegment},
    store::{Store, StoreError},
};
use crate::{
    config::{Config, Secrets},
    runtime::{self, InferenceView, SharedStatus},
};
use base64::Engine as _;
use chrono::{SecondsFormat, Utc};
use rumqttc::v5::{
    mqttbytes::{
        v5::{Filter, LastWill, Packet as MqttPacket, PublishProperties, SubscribeReasonCode},
        QoS,
    },
    AsyncClient, Event, MqttOptions,
};
use serde::{Deserialize, Serialize};
use std::{
    path::{Path, PathBuf},
    sync::Arc,
    time::{Duration, Instant},
};
use tokio_util::sync::CancellationToken;

const JOB_TOPIC: &str = "$share/audio-asr/orbit/v1/audio/jobs";
const MODEL_NAME: &str = "Qwen3-ASR-0.6B-Q8_0";

#[derive(Debug, Serialize)]
#[serde(rename_all = "snake_case")]
struct WorkerState<'a> {
    schema_version: u8,
    agent_id: &'a str,
    host_label: &'a str,
    model: &'a str,
    device: &'a str,
    status: &'a str,
    updated_at: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    queue: Option<WorkerQueue>,
}

#[derive(Debug, Serialize)]
struct WorkerQueue {
    queued: u32,
    processing: u32,
    outbox: u32,
    capacity: u32,
}

#[derive(Debug, Deserialize)]
struct Ack {
    schema_version: u8,
    job_id: String,
    status: String,
}

fn queue_path(agent_id: &str) -> PathBuf {
    dirs::data_dir()
        .unwrap_or_else(|| PathBuf::from("."))
        .join("Orbit")
        .join("inference")
        .join(format!("{agent_id}.sqlite3"))
}

fn worker_mqtt_options(c: &Config, s: &Secrets) -> Result<MqttOptions, String> {
    let mut options = runtime::mqtt_options(c, s, format!("orbit-asr-{}", c.agent_id))?;
    options.set_max_packet_size(Some(256 * 1024));
    options.set_last_will(LastWill::new(
        format!("orbit/v1/agents/{}/audio/state", c.agent_id),
        state_bytes(c, "offline", selected_device(c), None),
        QoS::AtLeastOnce,
        true,
        None,
    ));
    Ok(options)
}

fn selected_device(c: &Config) -> &'static str {
    match c.inference.device.as_str() {
        "cpu" => "cpu",
        "gpu" => "vulkan",
        _ => "vulkan",
    }
}

fn update_inference(status: &SharedStatus, f: impl FnOnce(&mut InferenceView)) {
    if let Ok(mut value) = status.lock() {
        f(&mut value.inference);
    }
}

fn now_string() -> String {
    Utc::now().to_rfc3339_opts(SecondsFormat::Millis, true)
}

fn state_bytes(c: &Config, state: &str, device: &str, store: Option<&Store>) -> Vec<u8> {
    serde_json::to_vec(&WorkerState {
        schema_version: 1,
        agent_id: &c.agent_id,
        host_label: &c.host_label,
        model: MODEL_NAME,
        device,
        status: state,
        updated_at: now_string(),
        queue: store
            .and_then(|s| s.counts().ok())
            .map(|counts| WorkerQueue {
                queued: counts.queued,
                processing: counts.processing,
                outbox: counts.outbox,
                capacity: c.inference.queue_capacity,
            }),
    })
    .unwrap_or_default()
}

fn publish(
    client: &AsyncClient,
    topic: String,
    retained: bool,
    payload: Vec<u8>,
) -> Result<(), String> {
    client
        .try_publish_with_properties(
            topic,
            QoS::AtLeastOnce,
            retained,
            payload,
            PublishProperties {
                content_type: Some("application/json".into()),
                ..Default::default()
            },
        )
        .map_err(|_| "MQTT 发送队列暂时不可用".into())
}

fn decode_audio(job: &Job, runtime_path: &Path) -> Result<Vec<f32>, String> {
    let mut decoder = Decoder::load(runtime_path)?;
    let mut samples = Vec::with_capacity(job.segment.duration_ms as usize * 16);
    for encoded in &job.segment.packets {
        let bytes = base64::engine::general_purpose::STANDARD
            .decode(encoded)
            .map_err(|_| "opus_packet_base64".to_string())?;
        let mut frame = vec![0i16; 1920];
        let decoded = decoder.decode(&bytes, &mut frame)?;
        if decoded != 320 {
            return Err("opus_frame_duration".into());
        }
        samples.extend(
            frame[..decoded]
                .iter()
                .map(|sample| *sample as f32 / 32768.0),
        );
    }
    if samples.len() != job.segment.duration_ms as usize * 16 {
        return Err("opus_duration".into());
    }
    Ok(samples)
}

fn result_for(
    job: &Job,
    agent_id: &str,
    device: &str,
    runtime_path: &Path,
    model: &SpeechModel,
) -> ResultMessage {
    let started = Instant::now();
    let completed_at = now_string();
    let base = |status: &str, text: String, language: String, code: String, message: String| {
        ResultMessage {
            schema_version: 1,
            job_id: job.job_id.clone(),
            segment_id: job.segment.segment_id.clone(),
            node_id: job.segment.node_id.clone(),
            agent_id: agent_id.to_string(),
            status: status.into(),
            text,
            language,
            segments: vec![],
            model: MODEL_NAME.into(),
            device: device.into(),
            processing_ms: started.elapsed().as_millis() as u64,
            completed_at,
            error_code: code,
            error_message: message,
        }
    };
    let audio = match decode_audio(job, runtime_path) {
        Ok(value) => value,
        Err(error) => {
            return base(
                "failed",
                String::new(),
                String::new(),
                "audio_decode".into(),
                error,
            )
        }
    };
    match model.transcribe(&audio, 16_000) {
        Ok(transcript) => {
            let mut value = base(
                "completed",
                transcript.text,
                transcript.language,
                String::new(),
                String::new(),
            );
            value.segments = vec![TextSegment {
                start_ms: 0,
                end_ms: job.segment.duration_ms,
                text: value.text.clone(),
            }];
            value
        }
        Err(error) => base(
            "failed",
            String::new(),
            String::new(),
            engine_error_code(&error).into(),
            error.to_string(),
        ),
    }
}

fn engine_error_code(error: &EngineError) -> &'static str {
    match error {
        EngineError::Cancelled => "cancelled",
        EngineError::MissingRuntime => "runtime_missing",
        EngineError::LoadLibrary => "runtime_load",
        EngineError::ApiVersion => "runtime_api",
        EngineError::MissingSymbol => "runtime_symbol",
        EngineError::InvalidText => "result_invalid",
        EngineError::Call(_) => "inference_failed",
    }
}

async fn process_one(
    store: &Arc<Store>,
    model: Arc<SpeechModel>,
    c: &Config,
    device: &str,
    status: &SharedStatus,
    result_tx: &tokio::sync::mpsc::Sender<Vec<u8>>,
) -> Result<(), String> {
    let Some((job, attempts)) = store.claim().map_err(|e| e.to_string())? else {
        return Ok(());
    };
    update_inference(status, |v| v.status = "processing".into());
    let worker_id = c.agent_id.clone();
    let job_copy = job.clone();
    let device_name = device.to_string();
    let runtime_path = assets::effective_runtime_path(&c.inference.runtime_path)
        .to_string_lossy()
        .into_owned();
    let result = tokio::task::spawn_blocking(move || {
        result_for(
            &job_copy,
            &worker_id,
            &device_name,
            Path::new(&runtime_path),
            &model,
        )
    })
    .await;
    let result = match result {
        Ok(result) => result,
        Err(_) => {
            store.retry(&job.job_id).map_err(|e| e.to_string())?;
            return Err("inference_task_panicked".into());
        }
    };
    if result.status == "failed" && attempts < 3 {
        store.retry(&job.job_id).map_err(|e| e.to_string())?;
        update_inference(status, |v| v.status = "ready".into());
        return Ok(());
    }
    // Persist first. The independent MQTT loop sends and retries the outbox.
    let payload = store
        .finish(&job.job_id, &result)
        .map_err(|e| e.to_string())?;
    // First delivery is immediate, even while older outbox entries are retried.
    // A full channel is safe: the durable outbox will retry the result.
    let _ = result_tx.try_send(payload);
    update_inference(status, |v| v.status = "ready".into());
    Ok(())
}

async fn publish_outbox(
    store: &Arc<Store>,
    client: &AsyncClient,
    agent_id: &str,
) -> Result<(), String> {
    for (_, payload) in store.outbox().map_err(|e| e.to_string())? {
        publish(
            client,
            format!("orbit/v1/agents/{agent_id}/audio/results"),
            false,
            payload,
        )?;
    }
    Ok(())
}

fn set_counts(store: &Store, status: &SharedStatus) {
    if let Ok(counts) = store.counts() {
        update_inference(status, |v| {
            v.queued = counts.queued + counts.processing;
            v.outbox = counts.outbox;
        });
    }
}

async fn process_queue(
    store: &Arc<Store>,
    model: Arc<SpeechModel>,
    c: &Config,
    status: &SharedStatus,
    cancel: &CancellationToken,
    result_tx: &tokio::sync::mpsc::Sender<Vec<u8>>,
) {
    let mut tick = tokio::time::interval(Duration::from_millis(250));
    tick.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
    loop {
        tokio::select! {
            biased;
            _ = cancel.cancelled() => break,
            _ = tick.tick() => {
                if !runtime::snapshot(status).inference.connected { continue; }
                if let Err(error) = process_one(store, model.clone(), c, selected_device(c), status, result_tx).await {
                    update_inference(status, |v| { v.status = "error".into(); v.error = error; });
                }
                set_counts(store, status);
            }
        }
    }
}

// Keep polling the connection while native inference is running. On shutdown,
// finish persisting an in-flight result before allowing the model to be reloaded.
async fn run_with_processor(
    network: impl std::future::Future<Output = ()>,
    processor: impl std::future::Future<Output = ()>,
    cancel: &CancellationToken,
) {
    tokio::join!(
        async {
            network.await;
            cancel.cancel();
        },
        processor
    );
}

pub async fn run(c: Config, s: Secrets, status: SharedStatus, cancel: CancellationToken) {
    let device = selected_device(&c);
    eprintln!(
        "Orbit native ASR starting: device={device}, model={}, runtime={}",
        c.inference.model_path,
        assets::effective_runtime_path(&c.inference.runtime_path).display()
    );
    update_inference(&status, |v| {
        v.status = "starting".into();
        v.device = device.into();
        v.model_id = if c.inference.model_id.is_empty() {
            assets::MODEL_ID.into()
        } else {
            c.inference.model_id.clone()
        };
        v.model_ready = false;
        v.error.clear();
    });
    let runtime_path = assets::effective_runtime_path(&c.inference.runtime_path);
    let runtime = match SpeechApi::load(&runtime_path) {
        Ok(value) => Arc::new(value),
        Err(error) => {
            eprintln!("Orbit native ASR runtime error: {error}");
            update_inference(&status, |v| {
                v.status = "error".into();
                v.error = error.to_string();
            });
            return;
        }
    };
    let model = match runtime.load_model(
        &c.inference.model_path,
        if c.inference.device == "gpu" {
            "gpu"
        } else {
            &c.inference.device
        },
    ) {
        Ok(value) => Arc::new(value),
        Err(error) => {
            eprintln!("Orbit native ASR model error: {error}");
            update_inference(&status, |v| {
                v.status = "error".into();
                v.error = error.to_string();
            });
            return;
        }
    };
    update_inference(&status, |v| v.model_ready = true);
    let queue = queue_path(&c.agent_id);
    if let Some(parent) = queue.parent() {
        if let Err(error) = std::fs::create_dir_all(parent) {
            eprintln!("Orbit native ASR queue directory error: {error}");
            update_inference(&status, |v| {
                v.status = "error".into();
                v.error = "queue_directory".into();
            });
            return;
        }
    }
    let store = match Store::open(&queue, c.inference.queue_capacity) {
        Ok(value) => Arc::new(value),
        Err(error) => {
            eprintln!("Orbit native ASR queue error: {error}");
            update_inference(&status, |v| {
                v.status = "error".into();
                v.error = error.to_string();
            });
            return;
        }
    };
    update_inference(&status, |v| v.status = "ready".into());
    if let Ok(mut value) = status.lock() {
        value.message = "ASR Worker 正在连接 MQTT".into();
    }
    let worker_cancel = cancel.child_token();
    let (result_tx, mut result_rx) = tokio::sync::mpsc::channel(16);
    run_with_processor(
        maintain_connection(&c, &s, &store, &status, &worker_cancel, &mut result_rx),
        process_queue(&store, model, &c, &status, &worker_cancel, &result_tx),
        &worker_cancel,
    )
    .await;
    update_inference(&status, |v| {
        v.connected = false;
        if v.status != "error" {
            v.status = "offline".into();
        }
    });
    if let Ok(mut value) = status.lock() {
        value.connected = false;
    }
}

async fn maintain_connection(
    c: &Config,
    s: &Secrets,
    store: &Arc<Store>,
    status: &SharedStatus,
    cancel: &CancellationToken,
    result_rx: &mut tokio::sync::mpsc::Receiver<Vec<u8>>,
) {
    let device = selected_device(c);
    let mut backoff = 1u64;
    'reconnect: loop {
        if cancel.is_cancelled() {
            break;
        }
        let options = match worker_mqtt_options(c, s) {
            Ok(value) => value,
            Err(error) => {
                eprintln!("Orbit native ASR MQTT config error: {error}");
                update_inference(status, |v| {
                    v.status = "error".into();
                    v.error = error;
                });
                break;
            }
        };
        let (client, mut eventloop) = AsyncClient::new(options, 128);
        let mut online = false;
        let mut last_state = String::new();
        let mut tick = tokio::time::interval(Duration::from_secs(5));
        let mut heartbeat = tokio::time::interval(Duration::from_secs(20));
        tick.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
        heartbeat.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
        loop {
            tokio::select! {
                _ = cancel.cancelled() => break 'reconnect,
                Some(payload) = result_rx.recv(), if online => {
                    let _ = publish(&client, format!("orbit/v1/agents/{}/audio/results", c.agent_id), false, payload);
                }
                _ = tick.tick(), if online => {
                    set_counts(store, status);
                    let _ = publish_outbox(store, &client, &c.agent_id).await;
                    let current = runtime::snapshot(status).inference.status;
                    if current != last_state && publish(&client, format!("orbit/v1/agents/{}/audio/state", c.agent_id), true, state_bytes(c, &current, device, Some(store))).is_ok() {
                        last_state = current;
                    }
                }
                _ = heartbeat.tick(), if online => {
                    let current = runtime::snapshot(status).inference.status;
                    let _ = publish(&client, format!("orbit/v1/agents/{}/audio/state", c.agent_id), true, state_bytes(c, &current, device, Some(store)));
                }
                event = eventloop.poll() => match event {
                    Ok(Event::Incoming(MqttPacket::ConnAck(_))) => {
                        backoff = 1;
                        let mut jobs = Filter::new(JOB_TOPIC, QoS::AtLeastOnce);
                        jobs.preserve_retain = true;
                        let acks = Filter::new(format!("orbit/v1/agents/{}/audio/acks", c.agent_id), QoS::AtLeastOnce);
                        if client.try_subscribe_many([jobs, acks]).is_err() { break; }
                    }
                    Ok(Event::Incoming(MqttPacket::SubAck(ack))) => {
                        if ack.return_codes.iter().any(|code| !matches!(code, SubscribeReasonCode::Success(_))) {
                            update_inference(status, |v| { v.status = "error".into(); v.error = "worker_acl".into(); });
                            break 'reconnect;
                        }
                        online = true;
                        update_inference(status, |v| { v.connected = true; if v.status != "processing" { v.status = "ready".into(); } v.error.clear(); });
                        if let Ok(mut value) = status.lock() { value.connected = true; value.message = "语音识别服务已连接".into(); }
                        let current = runtime::snapshot(status).inference.status;
                        let _ = publish(&client, format!("orbit/v1/agents/{}/audio/state", c.agent_id), true, state_bytes(c, &current, device, Some(store)));
                    }
                    Ok(Event::Incoming(MqttPacket::Publish(p))) => {
                        let acks_topic = format!("orbit/v1/agents/{}/audio/acks", c.agent_id);
                        if p.topic.as_ref() == acks_topic.as_bytes() {
                            if let Ok(ack) = serde_json::from_slice::<Ack>(&p.payload) {
                                if ack.schema_version == 1 && ack.status == "stored" { let _ = store.acknowledge(&ack.job_id); }
                            }
                        } else if p.topic.as_ref() == b"orbit/v1/audio/jobs" && !p.retain {
                            if let Ok(job) = decode_job(&p.payload) {
                                match store.accept(&job) {
                                    Ok(_) => { set_counts(store, status); }
                                    Err(StoreError::Conflict) => {}
                                    Err(error) => update_inference(status, |v| v.error = error.to_string()),
                                }
                            }
                        }
                    }
                    Ok(Event::Incoming(MqttPacket::Disconnect(_))) => break,
                    Err(_) => break,
                    _ => {}
                }
            }
        }
        update_inference(status, |v| v.connected = false);
        if let Ok(mut value) = status.lock() {
            value.connected = false;
            value.message = "ASR Worker 正在重连 MQTT".into();
        }
        tokio::select! { _ = cancel.cancelled() => break, _ = tokio::time::sleep(Duration::from_secs(backoff)) => {} }
        backoff = (backoff * 2).min(30);
    }
    update_inference(status, |v| {
        v.connected = false;
        if v.status != "error" {
            v.status = "offline".into();
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};

    #[test]
    fn heartbeat_reports_durable_queue_and_will_omits_it() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::open(&dir.path().join("queue.sqlite"), 1000).unwrap();
        let mut config = Config::default();
        config.inference.queue_capacity = 1000;
        let state: serde_json::Value =
            serde_json::from_slice(&state_bytes(&config, "ready", "cpu", Some(&store))).unwrap();
        assert_eq!(
            state["queue"],
            serde_json::json!({"queued":0,"processing":0,"outbox":0,"capacity":1000})
        );
        let will: serde_json::Value =
            serde_json::from_slice(&state_bytes(&config, "offline", "cpu", None)).unwrap();
        assert!(will.get("queue").is_none());
    }

    #[tokio::test]
    async fn slow_inference_keeps_network_alive_and_finishes_before_shutdown() {
        let cancel = CancellationToken::new();
        let heartbeats = AtomicUsize::new(0);
        let persisted = AtomicUsize::new(0);
        let (started_tx, started_rx) = tokio::sync::oneshot::channel();
        let (finish_tx, finish_rx) = tokio::sync::oneshot::channel();
        let network = async {
            started_rx.await.unwrap();
            for _ in 0..3 {
                tokio::task::yield_now().await;
                heartbeats.fetch_add(1, Ordering::SeqCst);
            }
            finish_tx.send(()).unwrap();
        };
        let processor = async {
            started_tx.send(()).unwrap();
            // Stand in for a native request that cannot finish until after heartbeats.
            finish_rx.await.unwrap();
            cancel.cancelled().await;
            persisted.fetch_add(1, Ordering::SeqCst);
        };
        tokio::time::timeout(
            Duration::from_secs(2),
            run_with_processor(network, processor, &cancel),
        )
        .await
        .unwrap();
        assert_eq!(heartbeats.load(Ordering::SeqCst), 3);
        assert_eq!(persisted.load(Ordering::SeqCst), 1);
    }

    #[tokio::test]
    async fn mqtt_publishes_processing_heartbeats_during_slow_inference() {
        use bytes::BytesMut;
        use rumqttc::v5::mqttbytes::{
            v5::{ConnAck, ConnectReturnCode, PubAck, SubAck},
            Error,
        };
        use tokio::{
            io::{AsyncReadExt, AsyncWriteExt},
            net::TcpListener,
        };

        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let mut config = Config::default();
        config.agent_id = "test-worker".into();
        config.mqtt.url = format!("mqtt://{}", listener.local_addr().unwrap());
        let secrets = Secrets::default();
        let dir = tempfile::tempdir().unwrap();
        let store = Arc::new(Store::open(&dir.path().join("queue.sqlite"), 10).unwrap());
        let status = Arc::new(std::sync::Mutex::new(runtime::Status::default()));
        let cancel = CancellationToken::new();
        let (finish_tx, finish_rx) = tokio::sync::oneshot::channel();
        let (result_tx, mut result_rx) = tokio::sync::mpsc::channel(16);
        let processor = async {
            // A processing model must not prevent MQTT handshakes or heartbeats.
            while !runtime::snapshot(&status).inference.connected {
                tokio::time::sleep(Duration::from_millis(10)).await;
            }
            update_inference(&status, |v| v.status = "processing".into());
            tokio::time::sleep(Duration::from_millis(500)).await;
            result_tx.send(b"test-result".to_vec()).await.unwrap();
            finish_rx.await.unwrap();
        };
        let broker = async {
            let (mut socket, _) = listener.accept().await.unwrap();
            let mut input = BytesMut::new();
            let started = Instant::now();
            let mut processing_reports = 0;
            let mut immediate_result = false;
            loop {
                let packet = loop {
                    match MqttPacket::read(&mut input, Some(256 * 1024)) {
                        Ok(packet) => break packet,
                        Err(Error::InsufficientBytes(_)) => {
                            assert!(socket.read_buf(&mut input).await.unwrap() > 0);
                        }
                        Err(error) => panic!("invalid packet: {error:?}"),
                    }
                };
                let reply = match packet {
                    MqttPacket::Connect(_, will, _) => {
                        let will = will.expect("offline last will");
                        let state: serde_json::Value =
                            serde_json::from_slice(&will.message).unwrap();
                        assert_eq!(state["status"], "offline");
                        assert!(will.retain);
                        MqttPacket::ConnAck(ConnAck {
                            session_present: false,
                            code: ConnectReturnCode::Success,
                            properties: None,
                        })
                    }
                    MqttPacket::Subscribe(sub) => MqttPacket::SubAck(SubAck {
                        pkid: sub.pkid,
                        return_codes: vec![
                            SubscribeReasonCode::Success(QoS::AtLeastOnce);
                            sub.filters.len()
                        ],
                        properties: None,
                    }),
                    MqttPacket::Publish(p) => {
                        if p.topic.as_ref() == b"orbit/v1/agents/test-worker/audio/results" {
                            assert_eq!(p.payload.as_ref(), b"test-result");
                            assert!(
                                started.elapsed() < Duration::from_secs(4),
                                "first delivery waited for the retry timer"
                            );
                            immediate_result = true;
                            let mut output = BytesMut::new();
                            MqttPacket::PubAck(PubAck::new(p.pkid, None))
                                .write(&mut output)
                                .unwrap();
                            socket.write_all(&output).await.unwrap();
                            continue;
                        }
                        assert_eq!(p.topic.as_ref(), b"orbit/v1/agents/test-worker/audio/state");
                        let value: serde_json::Value = serde_json::from_slice(&p.payload).unwrap();
                        if value["status"] == "processing" {
                            processing_reports += 1;
                        }
                        // Observe the periodic 20-second heartbeat, not just a status change.
                        if started.elapsed() >= Duration::from_secs(19) && processing_reports >= 2 {
                            assert!(immediate_result);
                            finish_tx.send(()).unwrap();
                            cancel.cancel();
                            break;
                        }
                        MqttPacket::PubAck(PubAck::new(p.pkid, None))
                    }
                    other => panic!("unexpected packet: {other:?}"),
                };
                let mut output = BytesMut::new();
                reply.write(&mut output).unwrap();
                socket.write_all(&output).await.unwrap();
            }
        };
        tokio::time::timeout(Duration::from_secs(25), async {
            tokio::join!(
                broker,
                run_with_processor(
                    maintain_connection(
                        &config,
                        &secrets,
                        &store,
                        &status,
                        &cancel,
                        &mut result_rx
                    ),
                    processor,
                    &cancel,
                )
            );
        })
        .await
        .unwrap();
        assert!(!runtime::snapshot(&status).inference.connected);
    }
}
