"""Prepare a DayTrace archive, then durably submit its audio through Core."""
from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import ssl
import sys
import tempfile
import threading
import time

import paho.mqtt.client as mqtt
from paho.mqtt.packettypes import PacketTypes
from paho.mqtt.properties import Properties

from . import protocol
from .runtime import DatabaseLock


def digest_file(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def atomic_write(path, data):
    path = Path(path)
    fd, temporary = tempfile.mkstemp(prefix=path.name + ".", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def json_bytes(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"),
                      allow_nan=False).encode("utf-8")


def read_plan(directory):
    directory = Path(directory)
    plan = json.loads((directory / "plan.json").read_text(encoding="utf-8"))
    if plan.get("schema_version") != 1:
        raise ValueError("unsupported import plan")
    protocol.identity(plan["node_id"])
    payload_file = directory / "segments.jsonl"
    if digest_file(payload_file) != plan["payload_sha256"]:
        raise ValueError("prepared payload checksum mismatch; refusing to publish changed data")
    items, seen = [], set()
    with payload_file.open(encoding="utf-8") as stream:
        for line in stream:
            item = json.loads(line)
            segment = item["segment"]
            protocol.job(protocol.encode({"schema_version": 1, "job_id": segment["segment_id"],
                                          "segment": segment}))
            if segment["node_id"] != plan["node_id"] or segment["segment_id"] in seen:
                raise ValueError("invalid node or duplicate segment in plan")
            seen.add(segment["segment_id"])
            items.append(item)
    if len(items) != plan["chunk_count"]:
        raise ValueError("prepared payload count mismatch")
    return plan, items


def prepare(archive, node_id, directory):
    from .daytrace import prepare_bundle

    archive, directory = Path(archive), Path(directory)
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    with DatabaseLock(directory / "import"):
        if (directory / "plan.json").exists():
            plan, _ = read_plan(directory)
            if plan["archive_sha256"] != digest_file(archive) or plan["node_id"] != node_id:
                raise ValueError("this directory belongs to a different archive or node")
            return plan
        summary, items = prepare_bundle(archive, node_id)
        data = b"".join(json_bytes(item) + b"\n" for item in items)
        plan = dict(summary, schema_version=1, archive_sha256=digest_file(archive),
                    node_id=node_id, chunk_count=len(items),
                    payload_sha256=hashlib.sha256(data).hexdigest())
        atomic_write(directory / "segments.jsonl", data)
        atomic_write(directory / "plan.json", json_bytes(plan))
        return plan


class ReceiptStore:
    """Only Core's stored ACK is a receipt; broker PUBACK never advances this file."""

    def __init__(self, directory, plan, items, broker):
        self.path = Path(directory) / "receipts.json"
        self.identity = {"schema_version": 1, "payload_sha256": plan["payload_sha256"],
                         "node_id": plan["node_id"], "broker": broker}
        self.known = {item["segment"]["segment_id"] for item in items}
        self.acked = set()
        if self.path.exists():
            saved = json.loads(self.path.read_text(encoding="utf-8"))
            if any(saved.get(key) != value for key, value in self.identity.items()):
                raise ValueError("receipts belong to a different plan or broker")
            if not isinstance(saved.get("acked"), list):
                raise ValueError("invalid receipts")
            self.acked = set(saved["acked"])
            if not self.acked <= self.known:
                raise ValueError("receipt contains unknown segment IDs")
        else:
            # Bind the destination before the first publish, even if its ACK is
            # lost and the process exits without acknowledging a single item.
            atomic_write(self.path, json_bytes(dict(self.identity, acked=[])))

    def mark(self, segment_id):
        if segment_id not in self.known:
            raise ValueError("unknown segment receipt")
        updated = self.acked | {segment_id}
        atomic_write(self.path, json_bytes(dict(self.identity, acked=sorted(updated))))
        self.acked = updated


def load_mqtt_config(path, node_id):
    value = json.loads(Path(path).read_text(encoding="utf-8"))
    config = value.get("mqtt", value).copy()
    if value.get("node_id", node_id) != node_id:
        raise ValueError("MQTT credential node does not match import plan")
    host = config.get("host")
    port = config.get("port", 8883)
    tls = config.get("tls", True)
    if not isinstance(host, str) or not host or "/" in host or "://" in host:
        raise ValueError("MQTT host must be a hostname or IP")
    if type(port) is not int or not 1 <= port <= 65535 or type(tls) is not bool:
        raise ValueError("invalid MQTT port or TLS setting")
    if not tls and host not in ("localhost", "127.0.0.1", "::1"):
        raise ValueError("TLS is required for a remote broker")
    password = os.environ.get(config.get("password_env", "ORBIT_MQTT_PASSWORD")) or config.get("password")
    if config.get("username") and not password:
        raise ValueError("MQTT password is missing")
    config.update(port=port, tls=tls, password=password)
    return config


class CorePublisher:
    def __init__(self, config, node_id):
        self.config = config
        self.topic = f"orbit/v1/nodes/{node_id}/audio/segments"
        self.ack_topic = f"orbit/v1/nodes/{node_id}/audio/acks"
        self.ready = threading.Event()
        self.received = threading.Event()
        self.expected = None
        self.error = None
        self.lock = threading.Lock()
        self.client = mqtt.Client(mqtt.CallbackAPIVersion.VERSION2, protocol=mqtt.MQTTv5,
                                  client_id=f"orbit-import-{os.urandom(8).hex()}")
        if config.get("username"):
            self.client.username_pw_set(config["username"], config["password"])
        if config["tls"]:
            self.client.tls_set(ca_certs=config.get("ca_file"), certfile=config.get("cert_file"),
                                keyfile=config.get("key_file"), cert_reqs=ssl.CERT_REQUIRED)
        self.client.on_connect = self._connect
        self.client.on_disconnect = self._disconnect
        self.client.on_subscribe = self._subscribe
        self.client.on_message = self._message
        self.client.reconnect_delay_set(1, 10)

    def _connect(self, client, userdata, flags, reason, properties):
        if reason.is_failure:
            self.error = "MQTT authentication/connection rejected"
            return
        result, _ = client.subscribe(self.ack_topic, qos=1)
        if result != mqtt.MQTT_ERR_SUCCESS:
            self.error = "cannot subscribe to Core acknowledgments"

    def _disconnect(self, client, userdata, flags, reason, properties):
        self.ready.clear()

    def _subscribe(self, client, userdata, mid, reasons, properties):
        if not reasons or any(reason.is_failure for reason in reasons):
            self.error = "MQTT acknowledgment subscription rejected"
        else:
            self.ready.set()

    def _message(self, client, userdata, message):
        if message.topic != self.ack_topic or message.retain:
            return
        try:
            value = protocol.loads(message.payload)
        except (ValueError, TypeError, UnicodeError):
            return
        with self.lock:
            if value.get("status") == "stored" and value.get("segment_id") == self.expected:
                self.received.set()

    def __enter__(self):
        try:
            self.client.connect(self.config["host"], self.config["port"], keepalive=30)
            self.client.loop_start()
            deadline = time.monotonic() + 30
            while not self.ready.wait(0.1):
                if self.error or time.monotonic() >= deadline:
                    raise RuntimeError(self.error or "MQTT connection/subscription timed out")
            return self
        except BaseException:
            self.client.disconnect()
            self.client.loop_stop()
            raise

    def __exit__(self, *args):
        self.client.disconnect()
        self.client.loop_stop()

    def submit(self, segment, timeout=10, attempts=5):
        with self.lock:
            self.expected = segment["segment_id"]
            self.received.clear()
        payload = protocol.encode(segment)
        properties = Properties(PacketTypes.PUBLISH)
        properties.ContentType = "application/json"
        properties.PayloadFormatIndicator = 1
        for _ in range(attempts):
            if self.error:
                raise RuntimeError(self.error)
            if not self.ready.wait(timeout):
                continue
            result = self.client.publish(self.topic, payload, qos=1, retain=False, properties=properties)
            if result.rc != mqtt.MQTT_ERR_SUCCESS:
                continue
            if self.received.wait(timeout):
                return
        raise RuntimeError(f"Core stored ACK timed out for {segment['segment_id']}; safe to resume")


def publish(directory, config_path, limit=None, rate=5.0, publisher_factory=CorePublisher):
    if not math.isfinite(rate) or rate <= 0 or rate > 100 or (limit is not None and limit < 1):
        raise ValueError("rate must be in (0, 100]; limit must be positive")
    directory = Path(directory)
    with DatabaseLock(directory / "import"):
        plan, items = read_plan(directory)
        config = load_mqtt_config(config_path, plan["node_id"])
        broker = {key: config[key] for key in ("host", "port", "tls")}
        receipts = ReceiptStore(directory, plan, items, broker)
        selected = [item for item in items if item["segment"]["segment_id"] not in receipts.acked]
        if limit is not None:
            selected = selected[:limit]
        initial = len(receipts.acked)
        print(json.dumps({"event": "start", "total": len(items), "already_stored": initial,
                          "selected": len(selected)}, ensure_ascii=False), flush=True)
        if selected:
            with publisher_factory(config, plan["node_id"]) as publisher:
                next_send = time.monotonic()
                for item in selected:
                    time.sleep(max(0, next_send - time.monotonic()))
                    next_send = time.monotonic() + 1 / rate
                    segment = item["segment"]
                    publisher.submit(segment)
                    receipts.mark(segment["segment_id"])
                    count = len(receipts.acked)
                    if count - initial <= 3 or count % 25 == 0 or count == initial + len(selected):
                        print(json.dumps({"event": "stored", "count": count, "total": len(items),
                                          "segment_id": segment["segment_id"]}), flush=True)
        result = {"total": len(items), "stored": len(receipts.acked),
                  "remaining": len(items) - len(receipts.acked), "newly_stored": len(receipts.acked) - initial}
        print(json.dumps({"event": "finished", **result}), flush=True)
        return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    preparation = commands.add_parser("prepare", help="validate and convert the whole archive without network")
    preparation.add_argument("archive", type=Path)
    preparation.add_argument("--node-id", required=True)
    preparation.add_argument("--output", type=Path, required=True)
    submission = commands.add_parser("publish", help="submit unacknowledged segments; safe to resume")
    submission.add_argument("--plan", type=Path, required=True)
    submission.add_argument("--mqtt-config", type=Path, required=True)
    submission.add_argument("--limit", type=int, help="send at most this many new segments")
    submission.add_argument("--rate", type=float, default=5, help="maximum new segments per second (default: 5)")
    inspection = commands.add_parser("status", help="show prepared/acknowledged counts without network")
    inspection.add_argument("--plan", type=Path, required=True)
    args = parser.parse_args()
    try:
        if args.command == "prepare":
            print(json.dumps(prepare(args.archive, args.node_id, args.output), ensure_ascii=False, indent=2))
        elif args.command == "publish":
            publish(args.plan, args.mqtt_config, args.limit, args.rate)
        else:
            plan, items = read_plan(args.plan)
            receipt_path = args.plan / "receipts.json"
            receipt = json.loads(receipt_path.read_text(encoding="utf-8")) if receipt_path.exists() else {}
            if receipt and receipt.get("payload_sha256") != plan["payload_sha256"]:
                raise ValueError("receipts belong to a different plan")
            count = len(receipt.get("acked", []))
            print(json.dumps({"node_id": plan["node_id"], "total": len(items), "stored": count,
                              "remaining": len(items) - count}, ensure_ascii=False))
    except KeyboardInterrupt:
        print("Interrupted; saved Core receipts can be resumed.", file=sys.stderr)
        return 130
    except (ValueError, OSError, RuntimeError) as exc:
        # Connection errors do not include credentials or transcript content.
        print(f"Import failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
