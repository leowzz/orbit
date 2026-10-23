"""Opt-in local broker + Core integration; never loads a model or starts services."""
import http.cookiejar
import json
import os
import sqlite3
import threading
import time
import urllib.parse
import urllib.request
import uuid

import paho.mqtt.client as mqtt
import pytest

from orbit_asr.protocol import encode
import orbit_asr.worker as worker_module
from orbit_asr.worker import Worker
from test_worker import fixture_job

pytestmark = pytest.mark.skipif(os.environ.get("ORBIT_ASR_MQTT_INTEGRATION") != "1",reason="set ORBIT_ASR_MQTT_INTEGRATION=1 with isolated local broker/Core")


def eventually(check, timeout=15):
    deadline = time.monotonic()+timeout
    while time.monotonic() < deadline:
        if check():
            return
        time.sleep(.05)
    raise AssertionError("integration condition timed out")


def test_node_core_worker_real_mqtt(tmp_path, monkeypatch):
    host = os.environ.get("ORBIT_ASR_TEST_MQTT_HOST","127.0.0.1")
    port = int(os.environ.get("ORBIT_ASR_TEST_MQTT_PORT","18894"))
    base = os.environ.get("ORBIT_ASR_TEST_CORE_URL","http://127.0.0.1:17620").rstrip("/")
    # This test publishes durable synthetic records; require an isolated loopback target.
    assert host in ("localhost","127.0.0.1","::1")
    assert urllib.parse.urlparse(base).hostname in ("localhost","127.0.0.1","::1")
    password = os.environ["ORBIT_ASR_TEST_CONSOLE_PASSWORD"]
    cookies = http.cookiejar.CookieJar()
    api = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookies))
    def get(path):
        with api.open(base+path,timeout=3) as response:
            return json.load(response)
    request = urllib.request.Request(base+"/api/auth/login",data=encode(dict(password=password)),headers={"Content-Type":"application/json","Origin":base})
    with api.open(request,timeout=3) as response:
        assert json.load(response)["authenticated"]

    suffix = uuid.uuid4().hex[:12]
    node_id,agent_id = "test-node-"+suffix,"test-asr-"+suffix
    item = fixture_job()
    item["segment"]["node_id"] = node_id
    segment_id = item["job_id"]
    counts = []
    class MockQwen:
        device = "cpu"
        def __init__(self,*args): pass
        def transcribe(self,audio):
            assert len(audio) == item["segment"]["duration_ms"]*16
            counts.append(1)
            return "Orbit local MQTT integration", "English"
    monkeypatch.setattr(worker_module,"resolve_device",lambda device:"cpu")
    worker = Worker(dict(agent_id=agent_id,model="mock-qwen-integration",device="cpu",database=str(tmp_path/"worker.sqlite"),result_retry_seconds=1,mqtt=dict(host=host,port=port,tls=False)),model_factory=MockQwen)
    jobs_seen = []
    original_message = worker.on_message
    def handled(client,userdata,message):
        original_message(client,userdata,message)
        if message.topic == "orbit/v1/audio/jobs":
            jobs_seen.append(1)
    worker.client.on_message = handled
    ready = threading.Event()
    original_subscribe = worker.on_subscribe
    def subscribed(*args):
        original_subscribe(*args)
        ready.set()
    worker.client.on_subscribe = subscribed
    node_ready = threading.Event()
    received = []
    node = mqtt.Client(mqtt.CallbackAPIVersion.VERSION2,client_id="test-node-"+suffix,protocol=mqtt.MQTTv5)
    def connected(client,userdata,flags,reason,properties):
        client.subscribe([(f"orbit/v1/nodes/{node_id}/audio/acks",1),(f"orbit/v1/agents/{agent_id}/audio/acks",1)])
    node.on_connect = connected
    node.on_subscribe = lambda *args: node_ready.set()
    node.on_message = lambda client,userdata,message: received.append((message.topic,json.loads(message.payload)))
    thread = threading.Thread(target=worker.run,daemon=True)
    try:
        node.connect(host,port,30)
        node.loop_start()
        assert node_ready.wait(5)
        thread.start()
        assert ready.wait(5)
        assert not worker.subscription_error
        properties = Worker.json_properties()
        topic = f"orbit/v1/nodes/{node_id}/audio/segments"
        node.publish(topic,encode(item["segment"]),qos=1,properties=properties).wait_for_publish(5)
        eventually(lambda:any(t.endswith("/audio/acks") and a.get("segment_id")==segment_id and a.get("status")=="stored" for t,a in received))
        def done():
            with worker.store.lock:
                row = worker.store.db.execute("SELECT status FROM jobs WHERE id=?",(segment_id,)).fetchone()
            return row and row[0] == "done"
        eventually(done)
        assert any(a.get("job_id")==segment_id and a.get("status")=="stored" for _,a in received)
        listing = get("/api/audio/segments?node_id="+node_id)
        assert listing["total"] == 1
        assert listing["items"][0]["text"] == "Orbit local MQTT integration"
        assert listing["items"][0]["status"] == "completed"
        detail = get("/api/audio/segments/"+segment_id)
        assert detail["result"]["agent_id"] == agent_id
        assert detail["result"]["segments"][0]["end_ms"] == item["segment"]["duration_ms"]
        assert not detail["segment"].get("packets")
        # Duplicate Node Segment is acknowledged without another ASR call.
        prior = len(received)
        node.publish(topic,encode(item["segment"]),qos=1,properties=properties).wait_for_publish(5)
        eventually(lambda:len(received)>prior)
        # Also exercise a replayed Core job against the worker tombstone directly.
        prior_jobs = len(jobs_seen)
        node.publish("orbit/v1/audio/jobs",encode(item),qos=1,properties=properties).wait_for_publish(5)
        eventually(lambda:len(jobs_seen)>prior_jobs)
        assert len(counts) == 1
        assert done()
        assert worker.store.outbox() == []
        assert get("/api/audio/segments?node_id="+node_id)["total"] == 1
        if os.environ.get("ORBIT_ASR_TEST_CORE_DB"):
            uri = "file:"+urllib.parse.quote(os.environ["ORBIT_ASR_TEST_CORE_DB"])+"?mode=ro"
            with sqlite3.connect(uri,uri=True) as db:
                assert db.execute("SELECT status FROM audio_jobs WHERE job_id=?",(segment_id,)).fetchone()[0] == "completed"
                assert db.execute("SELECT text FROM audio_results WHERE job_id=?",(segment_id,)).fetchone()[0] == "Orbit local MQTT integration"
    finally:
        worker.stop.set()
        if thread.is_alive():
            thread.join(5)
        node.disconnect()
        node.loop_stop()
        assert not thread.is_alive()
