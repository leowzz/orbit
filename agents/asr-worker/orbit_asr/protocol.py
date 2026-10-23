"""Orbit audio v1 validation. Reject poison input before touching the queue."""
import base64
import json
import re
import uuid
from datetime import datetime

MAX_BYTES = 128 * 1024
IDENTITY = re.compile(r"[a-z0-9][a-z0-9_-]{0,63}\Z")


def identity(value):
    if not isinstance(value, str) or not IDENTITY.fullmatch(value):
        raise ValueError("invalid identity")
    return value


def uid(value):
    if not isinstance(value, str) or str(uuid.UUID(value)) != value:
        raise ValueError("invalid UUID")


def number(value, minimum, maximum):
    if type(value) is not int or not minimum <= value <= maximum:
        raise ValueError("invalid integer range")


def loads(payload):
    if not 0 < len(payload) <= MAX_BYTES:
        raise ValueError("payload size outside 1..128 KiB")
    def unique(pairs):
        obj = {}
        for key, value in pairs:
            if key in obj:
                raise ValueError("duplicate JSON key")
            obj[key] = value
        return obj
    result = json.loads(payload, object_pairs_hook=unique)
    if not isinstance(result, dict) or type(result.get("schema_version")) is not int or result["schema_version"] != 1:
        raise ValueError("unsupported schema")
    return result


def job(payload):
    result = loads(payload)
    uid(result.get("job_id"))
    s = result.get("segment")
    if not isinstance(s, dict) or type(s.get("schema_version")) is not int or s["schema_version"] != 1:
        raise ValueError("invalid segment schema")
    for name in ("segment_id", "session_id"):
        uid(s.get(name))
    if result["job_id"] != s["segment_id"]:
        raise ValueError("job_id must equal segment_id")
    identity(s.get("node_id"))
    number(s.get("sequence"), 0, 2**63-1)
    number(s.get("duration_ms"), 20, 20000)
    number(s.get("speech_ms"), 0, s["duration_ms"])
    for key, expected in (("sample_rate",16000),("channels",1),("frame_ms",20)):
        if type(s.get(key)) is not int or s[key] != expected:
            raise ValueError("unsupported audio format")
    if s.get("codec") != "opus":
        raise ValueError("unsupported codec")
    stamp = s.get("started_at")
    if not isinstance(stamp, str) or not re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z", stamp):
        raise ValueError("timestamp must be UTC RFC3339")
    datetime.fromisoformat(stamp.replace("Z", "+00:00"))
    packets = s.get("packets")
    if not isinstance(packets, list) or not 1 <= len(packets) <= 1000 or len(packets)*20 != s["duration_ms"]:
        raise ValueError("invalid packet count")
    for packet in packets:
        if not isinstance(packet, str) or not 1 <= len(base64.b64decode(packet, validate=True)) <= 1275:
            raise ValueError("invalid Opus packet")
    return result


def encode(value):
    payload = json.dumps(value, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode()
    if len(payload) > MAX_BYTES:
        raise ValueError("result exceeds payload limit")
    return payload
