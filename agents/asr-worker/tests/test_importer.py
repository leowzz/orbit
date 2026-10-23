import base64
import hashlib
import json
from types import SimpleNamespace
import uuid

import pytest

from orbit_asr import importer


@pytest.fixture
def prepared(tmp_path):
    segments = []
    for index in range(3):
        segments.append({"source": {"audio_path": f"audio/{index}.opus"}, "segment": {
            "schema_version": 1, "segment_id": str(uuid.uuid4()), "session_id": str(uuid.uuid4()),
            "node_id": "daytrace-test", "sequence": index, "started_at": "2026-10-23T00:00:00Z",
            "duration_ms": 20, "speech_ms": 20, "sample_rate": 16000, "channels": 1,
            "codec": "opus", "frame_ms": 20, "packets": [base64.b64encode(b"packet").decode()],
        }})
    data = b"".join(importer.json_bytes(item) + b"\n" for item in segments)
    plan = {"schema_version": 1, "node_id": "daytrace-test", "chunk_count": 3,
            "payload_sha256": hashlib.sha256(data).hexdigest()}
    (tmp_path / "segments.jsonl").write_bytes(data)
    (tmp_path / "plan.json").write_bytes(importer.json_bytes(plan))
    config = tmp_path / "mqtt.json"
    config.write_text(json.dumps({"host": "localhost", "port": 1883, "tls": False,
                                  "node_id": "daytrace-test"}))
    return tmp_path, config, plan, segments


def test_publish_failure_retains_only_core_acknowledged_progress_and_resumes(prepared):
    directory, config, _, items = prepared
    calls = []

    class Publisher:
        def __init__(self, *args):
            pass

        def __enter__(self):
            return self

        def __exit__(self, *args):
            pass

        def submit(self, segment):
            calls.append(segment["segment_id"])
            if len(calls) == 2:
                raise RuntimeError("Core ACK lost after broker accepted the packet")

    with pytest.raises(RuntimeError, match="ACK lost"):
        importer.publish(directory, config, rate=100, publisher_factory=Publisher)
    receipts = json.loads((directory / "receipts.json").read_text())
    assert receipts["acked"] == [items[0]["segment"]["segment_id"]]
    result = importer.publish(directory, config, rate=100, publisher_factory=Publisher)
    assert result == {"total": 3, "stored": 3, "remaining": 0, "newly_stored": 2}
    assert calls[1] == calls[2]  # An unconfirmed submission reuses the exact segment identity.
    assert len(calls) == 4

    def forbidden(*args):
        pytest.fail("a completed plan must not reconnect or republish")

    assert importer.publish(directory, config, publisher_factory=forbidden)["newly_stored"] == 0


def test_limit_sends_new_items_without_skipping_remaining(prepared):
    directory, config, _, _ = prepared

    class Publisher:
        def __init__(self, *args): pass
        def __enter__(self): return self
        def __exit__(self, *args): pass
        def submit(self, segment): pass

    assert importer.publish(directory, config, limit=1, publisher_factory=Publisher)["stored"] == 1
    assert importer.publish(directory, config, limit=1, publisher_factory=Publisher)["stored"] == 2


def test_receipts_cannot_be_reused_for_another_broker_or_changed_payload(prepared):
    directory, _, plan, items = prepared
    broker = {"host": "localhost", "port": 1883, "tls": False}
    store = importer.ReceiptStore(directory, plan, items, broker)
    with pytest.raises(ValueError, match="different plan or broker"):
        importer.ReceiptStore(directory, plan, items, dict(broker, host="another-broker"))
    store.mark(items[0]["segment"]["segment_id"])
    with pytest.raises(ValueError, match="different plan or broker"):
        importer.ReceiptStore(directory, plan, items, dict(broker, port=1884))
    with (directory / "segments.jsonl").open("ab") as stream:
        stream.write(b"\n")
    with pytest.raises(ValueError, match="checksum mismatch"):
        importer.read_plan(directory)


def test_receipt_disk_failure_does_not_advance_memory_or_corrupt_prior_receipt(prepared, monkeypatch):
    directory, _, plan, items = prepared
    store = importer.ReceiptStore(directory, plan, items, {"host": "localhost"})
    first, second = (item["segment"]["segment_id"] for item in items[:2])
    store.mark(first)
    previous = store.path.read_bytes()
    monkeypatch.setattr(importer.os, "replace", lambda *args: (_ for _ in ()).throw(OSError("disk full")))
    with pytest.raises(OSError):
        store.mark(second)
    assert store.acked == {first}
    assert store.path.read_bytes() == previous
    assert not list(directory.glob("receipts.json.*"))


def test_only_matching_nonretained_core_stored_ack_completes_publish():
    publisher = importer.CorePublisher({"host": "localhost", "port": 1883, "tls": False}, "daytrace-test")
    publisher.expected = str(uuid.uuid4())
    valid = {"schema_version": 1, "segment_id": publisher.expected, "status": "stored"}

    def deliver(value, topic=None, retain=False):
        message = SimpleNamespace(payload=json.dumps(value).encode(), topic=topic or publisher.ack_topic,
                                  retain=retain)
        publisher._message(None, None, message)

    deliver(valid, retain=True)
    deliver(valid, topic="orbit/v1/agents/other/audio/acks")
    deliver(dict(valid, segment_id=str(uuid.uuid4())))
    deliver(dict(valid, status="completed"))
    deliver(dict(valid, schema_version=2))
    assert not publisher.received.is_set()
    deliver(valid)
    assert publisher.received.is_set()


def test_broker_puback_is_not_a_core_receipt(prepared):
    _, _, _, items = prepared
    publisher = importer.CorePublisher({"host": "localhost", "port": 1883, "tls": False}, "daytrace-test")
    payloads = []
    publisher.client = SimpleNamespace(publish=lambda topic, payload, **kwargs:
                                       (payloads.append(payload) or SimpleNamespace(rc=0)))
    publisher.ready.set()
    with pytest.raises(RuntimeError, match="Core stored ACK timed out"):
        publisher.submit(items[0]["segment"], timeout=0.001, attempts=2)
    assert len(payloads) == 2 and payloads[0] == payloads[1]


def test_remote_plaintext_and_wrong_node_are_rejected(prepared):
    _, config, _, _ = prepared
    config.write_text(json.dumps({"host": "mqtt.example.com", "tls": False}))
    with pytest.raises(ValueError, match="TLS is required"):
        importer.load_mqtt_config(config, "daytrace-test")
    config.write_text(json.dumps({"node_id": "other", "host": "localhost", "tls": False}))
    with pytest.raises(ValueError, match="node does not match"):
        importer.load_mqtt_config(config, "daytrace-test")
