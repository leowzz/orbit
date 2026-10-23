import argparse
from contextlib import redirect_stdout
import sys
import json
import logging
import os
import signal
import socket
import threading
import time
from datetime import datetime, timezone
from pathlib import Path

from .model import QwenModel, decode, resolve_device
from .protocol import identity, job, loads, uid, encode, number
from .store import Store
from .runtime import DatabaseLock, DatabaseInUseError, StatusEvents, read_control
from .settings import load_config, validate_config, get_password

LOG = logging.getLogger("orbit-asr")


def log_startup_failure(exc,stage,device):
    """Actionable diagnostics from known context, never raw exception contents."""
    missing = getattr(exc,"name","") or ""
    if isinstance(exc,(ImportError,ModuleNotFoundError)):
        if missing.split(".")[0] == "torch":
            LOG.error("未安装或无法加载 PyTorch；请运行 scripts/install.ps1 安装 CPU 或 CUDA 依赖")
        elif missing.split(".")[0] == "qwen_asr":
            LOG.error("未安装或无法加载 qwen-asr；请运行 scripts/install.ps1 安装模型依赖")
        else:
            LOG.error("模型依赖缺失或无法加载；请重新运行 scripts/install.ps1，并检查安装是否成功")
    elif stage == "device" and device == "cuda":
        LOG.error("CUDA 不可用或初始化失败；请选择 CPU，或检查 NVIDIA 驱动及 PyTorch CUDA 安装")
    elif stage == "model":
        LOG.error("模型加载失败；请检查模型名称或本地目录、下载网络、磁盘空间及可用内存")
    else:
        LOG.error("worker runtime failed error=%s",type(exc).__name__)


def now():
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


class Worker:
    def __init__(self, config, model_factory=QwenModel, decoder=decode, client=None, events=None, password=None):
        import paho.mqtt.client as mqtt
        self.events = events
        self._status = "starting"
        self.device_resolved = False
        self.mqtt_connected = False
        self.subscribed = False
        self.config = config
        number(config.get("queue_capacity", 1000), 1, 100000)
        number(config.get("max_attempts", 3), 1, 10)
        number(config.get("result_retry_seconds", 5), 1, 300)
        if config.get("device", "auto") not in ("auto", "cpu", "cuda"):
            raise ValueError("device must be auto, cpu or cuda")
        self.fatal = False
        for key, limit in (("model",256),("host_label",256)):
            value = config.get(key, socket.gethostname() if key == "host_label" else None)
            if not isinstance(value,str) or not value or len(value.encode("utf-8")) > limit:
                raise ValueError(f"{key} must be nonempty and at most {limit} UTF-8 bytes")
        self.subscription_error = False
        self.agent_id = identity(config["agent_id"])
        self.prefix = f"orbit/v1/agents/{self.agent_id}/audio"
        self.device = "cpu" if config.get("device", "auto") == "auto" else config["device"]
        if self.device not in ("cpu", "cuda"):
            raise ValueError("device must be auto, cpu or cuda")
        self.status = "starting"
        self.stop = threading.Event()
        self.model_factory = model_factory
        self.decoder = decoder
        db = Path(config.get("database", "data/asr.sqlite3"))
        db.parent.mkdir(parents=True, exist_ok=True)
        self.store = Store(db, config.get("queue_capacity", 1000))
        self.client = client or mqtt.Client(mqtt.CallbackAPIVersion.VERSION2, client_id=f"orbit-asr-{self.agent_id}", protocol=mqtt.MQTTv5, manual_ack=True)
        self.client.on_connect = self.on_connect
        self.client.on_disconnect = self.on_disconnect
        self.client.on_connect_fail = self.on_connect_fail
        self.client.on_message = self.on_message
        self.client.on_subscribe = self.on_subscribe
        self.client.reconnect_delay_set(min_delay=1, max_delay=30)
        self.client.max_queued_messages_set(50)
        self.client.will_set(self.prefix+"/state", encode(self.state("offline")), qos=1, retain=True, properties=self.json_properties(will=True))
        broker = config["mqtt"]
        if broker.get("username"):
            password = os.environ.get(broker.get("password_env", "ORBIT_MQTT_PASSWORD")) or password
            if not password:
                raise ValueError("MQTT password is not configured")
            self.client.username_pw_set(broker["username"], password)
        if broker.get("tls", True):
            self.client.tls_set(ca_certs=broker.get("ca_file"), certfile=broker.get("cert_file"), keyfile=broker.get("key_file"))

    @property
    def status(self):
        return self._status

    @status.setter
    def status(self,value):
        if hasattr(self,"stop") and self.stop.is_set() and value in ("ready","processing"):
            return
        changed = value != self._status
        self._status = value
        if changed:
            self.emit_status()

    def snapshot(self):
        return dict(status="error" if self.subscription_error and self.status != "offline" else self.status,
                    mqtt_connected=self.mqtt_connected,subscribed=self.subscribed,
                    device=self.device if self.device_resolved else None,model=self.config["model"],
                    queue=self.store.counts() if hasattr(self,"store") else dict(queued=0,processing=0,outbox=0))

    def emit_status(self):
        if self.events is not None:
            self.events.emit(self.snapshot())

    def state(self, status=None):
        return dict(schema_version=1, agent_id=self.agent_id, host_label=self.config.get("host_label",socket.gethostname()), model=self.config["model"], device=self.device, status=status or ("error" if self.subscription_error and self.status != "offline" else self.status), updated_at=now())

    @staticmethod
    def json_properties(will=False):
        from paho.mqtt.properties import Properties
        from paho.mqtt.packettypes import PacketTypes
        properties = Properties(PacketTypes.WILLMESSAGE if will else PacketTypes.PUBLISH)
        properties.PayloadFormatIndicator = 1
        properties.ContentType = "application/json"
        return properties

    def publish(self, topic, payload, retain=False):
        return self.client.publish(topic, payload, qos=1, retain=retain, properties=self.json_properties())

    def publish_state(self):
        self.publish(self.prefix+"/state", encode(self.state()), retain=True)

    def on_subscribe(self, client, userdata, mid, reason_codes, properties):
        self.subscribed = bool(reason_codes) and not any(code.is_failure for code in reason_codes)
        self.subscription_error = not self.subscribed
        if not self.subscribed:
            LOG.error("MQTT audio subscription refused; check worker ACL")
            self.publish_state()
        else:
            LOG.info("MQTT audio subscriptions ready")
        self.emit_status()

    def on_disconnect(self,client,userdata,disconnect_flags,reason_code,properties):
        self.mqtt_connected = False
        self.subscribed = False
        LOG.info("MQTT disconnected; reconnect will resume durable delivery")
        self.emit_status()

    def on_connect_fail(self,client,userdata):
        self.mqtt_connected = False
        self.subscribed = False
        LOG.warning("MQTT connection attempt failed; retrying")
        self.emit_status()

    def on_connect(self, client, userdata, flags, reason_code, properties):
        if reason_code.is_failure:
            self.mqtt_connected = False
            self.subscribed = False
            LOG.error("MQTT connection refused")
            self.emit_status()
            return
        self.mqtt_connected = True
        self.subscribed = False
        LOG.info("MQTT connected; requesting audio subscriptions")
        self.subscription_error = False
        client.subscribe([("$share/audio-asr/orbit/v1/audio/jobs",1),(self.prefix+"/acks",1)])
        self.publish_state()
        self.emit_status()

    def on_message(self, client, userdata, message):
        try:
            if message.retain:
                raise ValueError("retained audio input forbidden")
            if message.topic == "orbit/v1/audio/jobs":
                self.store.accept(job(message.payload))
            elif message.topic == self.prefix+"/acks":
                ack = loads(message.payload)
                uid(ack.get("job_id"))
                if ack.get("status") != "stored":
                    raise ValueError("invalid acknowledgement")
                self.store.acknowledge(ack["job_id"])
            else:
                raise ValueError("unexpected topic")
        except (ValueError, TypeError, KeyError, UnicodeError) as exc:
            LOG.warning("rejected invalid MQTT input: %s", type(exc).__name__)
        except Exception:
            # Never PUBACK an unpersisted valid job. Core also retries until result.
            LOG.exception("job persistence unavailable; input left unacknowledged")
            self.status = "error"
            return
        client.ack(message.mid, message.qos)
        self.emit_status()

    def process_one(self, model):
        if self.stop.is_set():
            return False
        claimed = self.store.claim()
        if not claimed:
            return False
        item, attempt = claimed
        segment = item["segment"]
        self.status = "processing"
        LOG.info("ASR processing job=%s attempt=%d",item["job_id"],attempt)
        started = time.monotonic()
        result = dict(schema_version=1,job_id=item["job_id"],segment_id=segment["segment_id"],node_id=segment["node_id"],agent_id=self.agent_id,status="completed",text="",language="",segments=[],model=self.config["model"],device=self.device,processing_ms=0,completed_at="",error_code="",error_message="")
        try:
            text, language = model.transcribe(self.decoder(segment))
            if (not isinstance(text,str) or not isinstance(language,str)
                    or len(text.encode("utf-8")) > 64000 or len(language.encode("utf-8")) > 64):
                raise ValueError("model returned transcript outside protocol limits")
            result.update(text=text, language=language, segments=[dict(start_ms=0,end_ms=segment["duration_ms"],text=text)])
            encode(result)
        except Exception as exc:
            LOG.warning("ASR attempt failed job=%s error=%s",item["job_id"],type(exc).__name__)
            if attempt < self.config.get("max_attempts",3):
                self.store.retry(item["job_id"])
                self.status = "error"
                self.stop.wait(min(2**attempt,30))
                return True
            # Avoid exception strings: model/library errors can include local secrets or audio.
            result.update(status="failed",text="",language="",segments=[],error_code="asr_failed",error_message=f"Audio processing failed ({type(exc).__name__})")
        result.update(processing_ms=min(86400000,int((time.monotonic()-started)*1000)),completed_at=now())
        self.store.finish(item["job_id"],result)
        LOG.info("ASR result queued job=%s status=%s processing_ms=%d",item["job_id"],result["status"],result["processing_ms"])
        self.status = "ready"
        return True

    def process_loop(self):
        model_loaded = False
        try:
            LOG.info("Loading ASR model device=%s",self.device)
            model = self.model_factory(self.config["model"],self.config.get("device","auto"))
            self.device = model.device
            self.device_resolved = True
            model_loaded = True
            LOG.info("ASR model ready device=%s",self.device)
            self.status = "ready"
            while not self.stop.is_set():
                if not self.process_one(model):
                    self.stop.wait(.2)
        except Exception as exc:
            log_startup_failure(exc,"processing" if model_loaded else "model",self.config.get("device","auto"))
            LOG.error("worker processing stopped; durable jobs preserved")
            self.status = "error"
            self.fatal = True
            self.stop.set()

    def run(self):
        self.emit_status()
        broker = self.config["mqtt"]
        processing = None
        network_started = False
        next_state = 0
        next_outbox = 0
        phase = "device"
        try:
            # Resolve before CONNECT so last will reports the selected hardware.
            self.device = resolve_device(self.config.get("device", "auto"))
            self.device_resolved = True
            self.emit_status()
            phase = "mqtt"
            if self.stop.is_set():
                return
            self.client.will_set(self.prefix+"/state", encode(self.state("offline")), qos=1, retain=True, properties=self.json_properties(will=True))
            self.client.connect_async(broker["host"],broker.get("port",8883),keepalive=30)
            self.client.loop_start()
            network_started = True
            processing = threading.Thread(target=self.process_loop, name="asr-processing",daemon=True)
            processing.start()
            while not self.stop.is_set():
                current = time.monotonic()
                if current >= next_state:
                    self.publish_state()
                    next_state = current+20
                if current >= next_outbox:
                    for (payload,) in self.store.outbox():
                        self.publish(self.prefix+"/results",payload)
                    next_outbox = current+self.config.get("result_retry_seconds",5)
                self.emit_status()
                self.stop.wait(1)
        except Exception as exc:
            log_startup_failure(exc,phase,self.config.get("device","auto"))
            self.fatal = True
            self.status = "error"
            self.stop.set()
        finally:
            self.stop.set()
            self.status = "error" if self.fatal else "offline"
            if processing is not None and processing.is_alive():
                LOG.info("Stopping process; in-flight model work remains durable for restart")
            if network_started:
                info = self.publish(self.prefix+"/state",encode(self.state()),retain=True)
                try:
                    info.wait_for_publish(timeout=2)
                except RuntimeError:
                    pass
                self.client.disconnect()
                self.client.loop_stop()
            self.mqtt_connected = False
            self.subscribed = False
            self.emit_status()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config",default="config.json")
    parser.add_argument("--gui-events",action="store_true")
    parser.add_argument("--control-stdin",action="store_true")
    args = parser.parse_args()
    logging.basicConfig(level=logging.INFO,format="%(asctime)s %(levelname)s %(message)s",stream=sys.stderr)
    events = StatusEvents() if args.gui_events else None
    initial = dict(status="starting",mqtt_connected=False,subscribed=False,device=None,model="",queue=dict(queued=0,processing=0,outbox=0))
    if events:
        events.emit(initial)
    try:
        config_path = Path(args.config).resolve()
        if not config_path.is_file():
            raise FileNotFoundError("config file missing")
        config = load_config(config_path)
        validate_config(config)
        database = Path(config.get("database","data/asr.sqlite3"))
        config["database"] = str(database if database.is_absolute() else config_path.parent/database)
        with DatabaseLock(config["database"]):
            password = None
            if config["mqtt"].get("username") and not os.environ.get(config["mqtt"].get("password_env","ORBIT_MQTT_PASSWORD")):
                password = get_password(config_path)
            worker = Worker(config,events=events,password=password)
            for sig in (signal.SIGINT,signal.SIGTERM):
                signal.signal(sig,lambda *_: worker.stop.set())
            if args.control_stdin:
                read_control(worker.stop)
            if events:
                # Model dependencies may print; reserve stdout for status protocol.
                with redirect_stdout(sys.stderr):
                    worker.run()
            else:
                worker.run()
            if worker.fatal:
                raise SystemExit(1)
    except Exception as exc:
        if isinstance(exc,DatabaseInUseError):
            LOG.error("Worker database is already in use; stop the other worker instance")
        else:
            LOG.error("Cannot start ASR worker error=%s",type(exc).__name__)
        if events:
            events.emit(dict(initial,status="error"))
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()
