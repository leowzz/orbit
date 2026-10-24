use base64::Engine;
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use sha2::{Digest, Sha256};
use std::{collections::HashSet, fmt};
use uuid::Uuid;

pub const MAX_MESSAGE_BYTES: usize = 128 * 1024;
pub const MAX_PACKET_BYTES: usize = 1275;
pub const MAX_PACKETS: usize = 1000;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Segment {
    pub schema_version: u8,
    pub segment_id: String,
    pub session_id: String,
    pub node_id: String,
    pub sequence: u64,
    pub started_at: String,
    pub duration_ms: u32,
    pub speech_ms: u32,
    pub sample_rate: u32,
    pub channels: u8,
    pub codec: String,
    pub frame_ms: u8,
    pub packets: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct Job {
    pub schema_version: u8,
    pub job_id: String,
    pub segment: Segment,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct TextSegment {
    pub start_ms: u32,
    pub end_ms: u32,
    pub text: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ResultMessage {
    pub schema_version: u8,
    pub job_id: String,
    pub segment_id: String,
    pub node_id: String,
    pub agent_id: String,
    pub status: String,
    pub text: String,
    pub language: String,
    pub segments: Vec<TextSegment>,
    pub model: String,
    pub device: String,
    pub processing_ms: u64,
    pub completed_at: String,
    pub error_code: String,
    pub error_message: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProtocolError(&'static str);

impl fmt::Display for ProtocolError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.0)
    }
}
impl std::error::Error for ProtocolError {}

fn identity(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 64
        && value.as_bytes()[0].is_ascii_alphanumeric()
        && value
            .bytes()
            .all(|c| c.is_ascii_alphanumeric() || c == b'_' || c == b'-')
}

fn uuid(value: &str) -> bool {
    Uuid::parse_str(value).is_ok_and(|u| u.to_string() == value)
}

fn utc_timestamp(value: &str) -> bool {
    value.ends_with('Z')
        && chrono::DateTime::parse_from_rfc3339(value)
            .is_ok_and(|t| t.offset().local_minus_utc() == 0)
}

fn unique_value<'de, R: serde_json::de::Read<'de>>(
    de: &mut serde_json::Deserializer<R>,
) -> Result<Value, serde_json::Error> {
    struct Visitor;
    impl<'de> serde::de::Visitor<'de> for Visitor {
        type Value = Value;
        fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
            f.write_str("JSON value")
        }
        fn visit_unit<E>(self) -> Result<Value, E>
        where
            E: serde::de::Error,
        {
            Ok(Value::Null)
        }
        fn visit_bool<E>(self, v: bool) -> Result<Value, E>
        where
            E: serde::de::Error,
        {
            Ok(Value::Bool(v))
        }
        fn visit_i64<E>(self, v: i64) -> Result<Value, E>
        where
            E: serde::de::Error,
        {
            Ok(Value::Number(v.into()))
        }
        fn visit_u64<E>(self, v: u64) -> Result<Value, E>
        where
            E: serde::de::Error,
        {
            Ok(Value::Number(v.into()))
        }
        fn visit_f64<E>(self, v: f64) -> Result<Value, E>
        where
            E: serde::de::Error,
        {
            serde_json::Number::from_f64(v)
                .map(Value::Number)
                .ok_or_else(|| E::custom("non-finite number"))
        }
        fn visit_str<E>(self, v: &str) -> Result<Value, E>
        where
            E: serde::de::Error,
        {
            Ok(Value::String(v.into()))
        }
        fn visit_string<E>(self, v: String) -> Result<Value, E>
        where
            E: serde::de::Error,
        {
            Ok(Value::String(v))
        }
        fn visit_seq<A>(self, mut seq: A) -> Result<Value, A::Error>
        where
            A: serde::de::SeqAccess<'de>,
        {
            let mut out = Vec::new();
            while let Some(value) = seq.next_element_seed(Seed)? {
                out.push(value);
            }
            Ok(Value::Array(out))
        }
        fn visit_map<A>(self, mut map: A) -> Result<Value, A::Error>
        where
            A: serde::de::MapAccess<'de>,
        {
            let mut out = Map::new();
            let mut keys = HashSet::new();
            while let Some(key) = map.next_key::<String>()? {
                if !keys.insert(key.clone()) {
                    return Err(serde::de::Error::custom("duplicate JSON key"));
                }
                let value = map.next_value_seed(Seed)?;
                out.insert(key, value);
            }
            Ok(Value::Object(out))
        }
    }
    struct Seed;
    impl<'de> serde::de::DeserializeSeed<'de> for Seed {
        type Value = Value;
        fn deserialize<D: serde::de::Deserializer<'de>>(self, d: D) -> Result<Value, D::Error> {
            d.deserialize_any(Visitor)
        }
    }
    serde::de::Deserializer::deserialize_any(de, Visitor)
}

pub fn decode_job(bytes: &[u8]) -> Result<Job, ProtocolError> {
    if bytes.is_empty() || bytes.len() > MAX_MESSAGE_BYTES {
        return Err(ProtocolError("payload size outside 1..128 KiB"));
    }
    let mut de = serde_json::Deserializer::from_slice(bytes);
    let value = unique_value(&mut de).map_err(|_| ProtocolError("invalid JSON"))?;
    de.end().map_err(|_| ProtocolError("trailing JSON"))?;
    let job: Job =
        serde_json::from_value(value).map_err(|_| ProtocolError("invalid job schema"))?;
    validate_job(&job)?;
    Ok(job)
}

pub fn validate_job(job: &Job) -> Result<(), ProtocolError> {
    let s = &job.segment;
    if job.schema_version != 1
        || !uuid(&job.job_id)
        || s.schema_version != 1
        || job.job_id != s.segment_id
        || !uuid(&s.segment_id)
        || !uuid(&s.session_id)
        || !identity(&s.node_id)
        || !utc_timestamp(&s.started_at)
    {
        return Err(ProtocolError("invalid job identity or timestamp"));
    }
    if s.sequence > i64::MAX as u64
        || !(20..=20_000).contains(&s.duration_ms)
        || s.speech_ms > s.duration_ms
        || s.sample_rate != 16_000
        || s.channels != 1
        || s.codec != "opus"
        || s.frame_ms != 20
        || s.packets.is_empty()
        || s.packets.len() > MAX_PACKETS
        || s.duration_ms != s.packets.len() as u32 * 20
    {
        return Err(ProtocolError("unsupported audio format"));
    }
    for packet in &s.packets {
        let bytes = base64::engine::general_purpose::STANDARD
            .decode(packet)
            .map_err(|_| ProtocolError("invalid Opus packet"))?;
        if bytes.is_empty() || bytes.len() > MAX_PACKET_BYTES {
            return Err(ProtocolError("invalid Opus packet"));
        }
    }
    Ok(())
}

pub fn encode<T: Serialize>(value: &T) -> Result<Vec<u8>, ProtocolError> {
    let bytes = serde_json::to_vec(value).map_err(|_| ProtocolError("cannot encode JSON"))?;
    if bytes.len() > MAX_MESSAGE_BYTES {
        return Err(ProtocolError("result exceeds payload limit"));
    }
    Ok(bytes)
}

fn canonical(value: &Value, out: &mut String) {
    match value {
        Value::Object(map) => {
            out.push('{');
            let mut keys: Vec<_> = map.keys().collect();
            keys.sort();
            for (i, key) in keys.iter().enumerate() {
                if i > 0 {
                    out.push(',');
                }
                out.push_str(&serde_json::to_string(key).unwrap());
                out.push(':');
                canonical(&map[*key], out);
            }
            out.push('}');
        }
        Value::Array(values) => {
            out.push('[');
            for (i, value) in values.iter().enumerate() {
                if i > 0 {
                    out.push(',');
                }
                canonical(value, out);
            }
            out.push(']');
        }
        _ => out.push_str(&serde_json::to_string(value).unwrap()),
    }
}

pub fn digest<T: Serialize>(value: &T) -> Result<String, ProtocolError> {
    let value = serde_json::to_value(value).map_err(|_| ProtocolError("cannot encode JSON"))?;
    let mut canonical_json = String::new();
    canonical(&value, &mut canonical_json);
    Ok(hex::encode(Sha256::digest(canonical_json.as_bytes())))
}

#[cfg(test)]
mod tests {
    use super::*;
    fn job(frames: usize) -> Job {
        let id = "00000000-0000-4000-8000-000000000001".to_string();
        Job {
            schema_version: 1,
            job_id: id.clone(),
            segment: Segment {
                schema_version: 1,
                segment_id: id,
                session_id: "00000000-0000-4000-8000-000000000002".into(),
                node_id: "android-1".into(),
                sequence: 0,
                started_at: "2026-10-24T00:00:00Z".into(),
                duration_ms: (frames * 20) as u32,
                speech_ms: (frames * 20) as u32,
                sample_rate: 16_000,
                channels: 1,
                codec: "opus".into(),
                frame_ms: 20,
                packets: vec!["AQ==".into(); frames],
            },
        }
    }
    #[test]
    fn rejects_duplicate_keys_and_boolean_numbers() {
        assert!(decode_job(br#"{"schema_version":1,"schema_version":1}"#).is_err());
        assert!(decode_job(br#"{"schema_version":1,"job_id":true}"#).is_err());
    }
    #[test]
    fn accepts_twenty_seconds_and_rejects_the_next_frame() {
        assert!(validate_job(&job(1000)).is_ok());
        assert!(validate_job(&job(1001)).is_err());
    }
}
