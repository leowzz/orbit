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
        v5::{Filter, Packet as MqttPacket, PublishProperties, SubscribeReasonCode},
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

fn state_bytes(c: &Config, state: &str, device: &str) -> Vec<u8> {
    serde_json::to_vec(&WorkerState {
        schema_version: 1,
        agent_id: &c.agent_id,
        host_label: &c.host_label,
        model: MODEL_NAME,
        device,
        status: state,
        updated_at: now_string(),
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
    client: &AsyncClient,
    device: &str,
    status: &SharedStatus,
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
    .await
    .map_err(|_| "inference_task_panicked".to_string())?;
    if result.status == "failed" && attempts < 3 {
        store.retry(&job.job_id).map_err(|e| e.to_string())?;
        update_inference(status, |v| v.status = "ready".into());
        return Ok(());
    }
    let payload = store
        .finish(&job.job_id, &result)
        .map_err(|e| e.to_string())?;
    publish(
        client,
        format!("orbit/v1/agents/{}/audio/results", c.agent_id),
        false,
        payload,
    )?;
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
    let mut backoff = 1u64;
    'reconnect: loop {
        if cancel.is_cancelled() {
            break;
        }
        let options = match worker_mqtt_options(&c, &s) {
            Ok(value) => value,
            Err(error) => {
                eprintln!("Orbit native ASR MQTT config error: {error}");
                update_inference(&status, |v| {
                    v.status = "error".into();
                    v.error = error;
                });
                break;
            }
        };
        let (client, mut eventloop) = AsyncClient::new(options, 128);
        let mut online = false;
        let mut tick = tokio::time::interval(Duration::from_secs(5));
        let mut heartbeat = tokio::time::interval(Duration::from_secs(20));
        tick.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
        heartbeat.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
        loop {
            tokio::select! {
                _ = cancel.cancelled() => break 'reconnect,
                _ = tick.tick(), if online => {
                    set_counts(&store, &status);
                    let _ = publish_outbox(&store, &client, &c.agent_id).await;
                    let _ = process_one(&store, model.clone(), &c, &client, device, &status).await;
                }
                _ = heartbeat.tick(), if online => {
                    let _ = publish(&client, format!("orbit/v1/agents/{}/audio/state", c.agent_id), true, state_bytes(&c, "ready", device));
                }
                event = eventloop.poll() => match event {
                    Ok(Event::Incoming(MqttPacket::ConnAck(_))) => {
                        online = true;
                        backoff = 1;
                        update_inference(&status, |v| { v.connected = true; v.status = "ready".into(); v.error.clear(); });
                        if let Ok(mut value) = status.lock() { value.connected = true; value.message = "ASR Worker 已连接 MQTT".into(); }
                        let mut jobs = Filter::new(JOB_TOPIC, QoS::AtLeastOnce);
                        jobs.preserve_retain = true;
                        let acks = Filter::new(format!("orbit/v1/agents/{}/audio/acks", c.agent_id), QoS::AtLeastOnce);
                        if client.try_subscribe_many([jobs, acks]).is_err() { break; }
                        let _ = publish(&client, format!("orbit/v1/agents/{}/audio/state", c.agent_id), true, state_bytes(&c, "ready", device));
                    }
                    Ok(Event::Incoming(MqttPacket::SubAck(ack))) => {
                        if ack.return_codes.iter().any(|code| !matches!(code, SubscribeReasonCode::Success(_))) {
                            update_inference(&status, |v| { v.status = "error".into(); v.error = "worker_acl".into(); });
                            break 'reconnect;
                        }
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
                                    Ok(_) => { let _ = process_one(&store, model.clone(), &c, &client, device, &status).await; set_counts(&store, &status); }
                                    Err(StoreError::Conflict) => {}
                                    Err(error) => update_inference(&status, |v| v.error = error.to_string()),
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
        update_inference(&status, |v| v.connected = false);
        if let Ok(mut value) = status.lock() {
            value.connected = false;
            value.message = "ASR Worker 正在重连 MQTT".into();
        }
        tokio::select! { _ = cancel.cancelled() => break, _ = tokio::time::sleep(Duration::from_secs(backoff)) => {} }
        backoff = (backoff * 2).min(30);
    }
    update_inference(&status, |v| {
        v.connected = false;
        v.status = "offline".into();
    });
}
