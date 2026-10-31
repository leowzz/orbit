import base64
import json
import sqlite3
import uuid
from types import SimpleNamespace

import av
import numpy as np
import pytest

from orbit_asr.model import decode
from orbit_asr.protocol import job, encode
from orbit_asr.store import Store
from orbit_asr.worker import Worker


def fixture_job():
    encoder = av.CodecContext.create("libopus", "w")
    encoder.sample_rate = 16000
    encoder.layout = "mono"
    encoder.format = "s16"
    encoder.bit_rate = 24000
    encoder.options = {"application":"voip", "frame_duration":"20"}
    packets = []
    # Encoder flush emits an extra padded frame; protocol includes whole packets.
    for i in range(3):
        audio = (np.sin(np.arange(320)*.1)*4000).astype(np.int16).reshape(1,-1)
        frame = av.AudioFrame.from_ndarray(audio,format="s16",layout="mono")
        frame.sample_rate = 16000
        frame.pts = i*320
        packets.extend(encoder.encode(frame))
    packets.extend(encoder.encode(None))
    segment_id = str(uuid.uuid4())
    return dict(schema_version=1,job_id=segment_id,segment=dict(schema_version=1,segment_id=segment_id,session_id=str(uuid.uuid4()),node_id="android-1",sequence=0,started_at="2026-10-23T12:00:00Z",duration_ms=len(packets)*20,speech_ms=40,sample_rate=16000,channels=1,codec="opus",frame_ms=20,packets=[base64.b64encode(bytes(x)).decode() for x in packets]))


def test_real_raw_opus_decode():
    item = job(encode(fixture_job()))
    audio = decode(item["segment"])
    assert len(audio) == item["segment"]["duration_ms"]*16
    assert audio.dtype == np.float32
    assert np.max(np.abs(audio)) > .01


def test_poison_validation():
    item = fixture_job()
    item["segment"]["duration_ms"] = True
    with pytest.raises(ValueError):
        job(encode(item))
    with pytest.raises(ValueError):
        job(b"x"*131073)
    with pytest.raises(ValueError):
        job(b'{"schema_version":1,"schema_version":1}')


@pytest.mark.parametrize("frames",[500,501,999,1000,1001])
def test_segment_twenty_second_limit(frames):
    item = fixture_job()
    segment = item["segment"]
    segment["packets"] = [segment["packets"][0]]*frames
    segment["duration_ms"] = frames*20
    if frames > 1000:
        with pytest.raises(ValueError):
            job(encode(item))
    else:
        assert job(encode(item)) == item
        assert len(decode(segment)) == segment["duration_ms"]*16


def test_durable_recovery_outbox_tombstone_capacity(tmp_path):
    path = tmp_path/"queue.sqlite"
    item = fixture_job()
    store = Store(path,1)
    assert store.accept(item)
    assert not store.accept(json.loads(json.dumps(item,sort_keys=True)))
    with pytest.raises(OverflowError):
        store.accept(fixture_job())
    assert store.claim()[1] == 1
    store.db.close()
    store = Store(path,1)
    assert store.claim()[1] == 2
    result = {"job_id":item["job_id"],"status":"completed"}
    store.finish(item["job_id"],result)
    store.acknowledge("unknown")
    assert len(store.outbox()) == 1
    store.db.close()
    store = Store(path,1)
    assert len(store.outbox()) == 1
    store.acknowledge(item["job_id"])
    assert store.outbox() == []
    assert not store.accept(item)
    item["segment"]["sequence"] += 1
    with pytest.raises(ValueError):
        store.accept(item)


class Client:
    def __init__(self):
        self.acks = []
        self.messages = []
    def reconnect_delay_set(self,**kw): pass
    def max_queued_messages_set(self,*args): pass
    def will_set(self,*args,**kw): pass
    def ack(self,*args): self.acks.append(args)
    def publish(self,*args,**kw):
        self.messages.append((args,kw))
        return SimpleNamespace(rc=0)
    def subscribe(self,*args): self.subscriptions = args


def worker(tmp_path):
    client = Client()
    config = dict(agent_id="sdt-asr",model="mock",device="cpu",database=str(tmp_path/"queue.sqlite"),max_attempts=1,queue_capacity=1,mqtt=dict(host="localhost",tls=False))
    return Worker(config,decoder=decode,client=client), client


def deliver(worker,client,item):
    worker.on_message(client,None,SimpleNamespace(topic="orbit/v1/audio/jobs",payload=encode(item),retain=False,mid=3,qos=1))


def test_callback_persists_before_ack_and_mock_model(tmp_path):
    w,c = worker(tmp_path)
    item = fixture_job()
    deliver(w,c,item)
    assert c.acks == [(3,1)]
    class Model:
        def transcribe(self,audio):
            assert len(audio)>0
            return "测试", "Chinese"
    assert w.process_one(Model())
    result = json.loads(w.store.outbox()[0][0])
    assert result["text"] == "测试"
    assert result["segments"][0]["end_ms"] == item["segment"]["duration_ms"]
    deliver(w,c,item)
    assert w.store.claim() is None
    # Full pending result queue leaves a new job unacknowledged.
    deliver(w,c,fixture_job())
    assert len(c.acks) == 2
    w.on_connect(c,None,None,SimpleNamespace(is_failure=False),None)
    assert "$share/audio-asr/orbit/v1/audio/jobs" == c.subscriptions[0][0][0]
    w.on_message(c,None,SimpleNamespace(topic=w.prefix+"/acks",payload=encode(dict(schema_version=1,job_id=item["job_id"],status="stored")),retain=False,mid=4,qos=1))
    assert w.store.outbox() == []


def test_model_failure_becomes_durable_failed_result(tmp_path):
    w,c = worker(tmp_path)
    deliver(w,c,fixture_job())
    class Model:
        def transcribe(self,audio): raise RuntimeError("private information")
    w.process_one(Model())
    result = json.loads(w.store.outbox()[0][0])
    assert result["status"] == "failed"
    assert result["text"] == ""
    assert "private" not in result["error_message"]


@pytest.mark.parametrize("failed",[False,True],ids=["completed","failed"])
def test_result_published_immediately_after_commit(tmp_path,monkeypatch,failed):
    w,c = worker(tmp_path)
    item = fixture_job()
    deliver(w,c,item)
    original_publish = c.publish
    def publish(topic,payload,**kwargs):
        assert topic == w.prefix+"/results"
        # An independent connection proves commit precedes the first send.
        with sqlite3.connect(w.config["database"]) as db:
            assert db.execute("SELECT status,result FROM jobs WHERE id=?",(item["job_id"],)).fetchone() == ("outbox",payload)
        return original_publish(topic,payload,**kwargs)
    monkeypatch.setattr(c,"publish",publish)
    class Model:
        def transcribe(self,audio):
            if failed:
                raise RuntimeError("private failure")
            return "立即回传","Chinese"
    # No run loop or outbox timer: completion itself must publish.
    assert w.process_one(Model())
    assert len(c.messages) == 1
    args,kwargs = c.messages[0]
    result = json.loads(args[1])
    assert result["job_id"] == item["job_id"]
    assert result["status"] == ("failed" if failed else "completed")
    assert kwargs["qos"] == 1 and kwargs["retain"] is False
    assert kwargs["properties"].ContentType == "application/json"
    assert w.store.outbox() == [(args[1],)]


@pytest.mark.parametrize("failure",["exception","disconnected","queue_full"])
def test_immediate_publish_failure_preserves_result_for_retry(tmp_path,monkeypatch,failure):
    w,c = worker(tmp_path)
    item = fixture_job()
    deliver(w,c,item)
    original_publish = c.publish
    def unavailable(*args,**kwargs):
        if failure == "exception":
            raise OSError("private connection detail")
        return SimpleNamespace(rc=4 if failure == "disconnected" else 15)
    monkeypatch.setattr(c,"publish",unavailable)
    class Model:
        def transcribe(self,audio): return "saved","English"
    assert w.process_one(Model())
    assert not w.fatal and not w.stop.is_set()
    payload = w.store.outbox()[0][0]
    assert json.loads(payload)["status"] == "completed"
    w.store.db.close()
    w.store = Store(w.config["database"])
    assert w.store.claim() is None
    assert w.store.outbox() == [(payload,)]
    monkeypatch.setattr(c,"publish",original_publish)
    w.publish_result(w.store.outbox()[0][0])
    assert c.messages[0][0] == (w.prefix+"/results",payload)
    assert w.store.outbox() == [(payload,)]
    w.on_message(c,None,SimpleNamespace(topic=w.prefix+"/acks",payload=encode(dict(schema_version=1,job_id=item["job_id"],status="stored")),retain=False,mid=4,qos=1))
    assert w.store.outbox() == []


@pytest.mark.parametrize("text,language",[("x"*64001,"English"),("hi","x"*65),("你"*22000,"Chinese")],
                         ids=["oversized-text", "oversized-language", "oversized-utf8"])
def test_oversized_model_output_becomes_failed(tmp_path,text,language):
    w,c = worker(tmp_path)
    deliver(w,c,fixture_job())
    class Model:
        def transcribe(self,audio): return text,language
    w.process_one(Model())
    result = json.loads(w.store.outbox()[0][0])
    assert result["status"] == "failed"
    assert result["text"] == ""


def test_subscription_rejection_and_json_properties(tmp_path):
    w,c = worker(tmp_path)
    w.on_subscribe(c,None,1,[SimpleNamespace(is_failure=True)],None)
    assert w.state()["status"] == "error"
    args,kwargs = c.messages[-1]
    assert kwargs["properties"].ContentType == "application/json"
    assert kwargs["properties"].PayloadFormatIndicator == 1


@pytest.mark.parametrize("key,value",[("model","a"*257),("host_label","你"*86)])
def test_config_rejects_oversized_utf8_fields(tmp_path,key,value):
    config = dict(agent_id="asr",model="mock",database=str(tmp_path/"db"),mqtt=dict(tls=False))
    config[key] = value
    with pytest.raises(ValueError):
        Worker(config,client=Client())


@pytest.mark.parametrize("environment,expected",[("environment-secret","environment-secret"),(None,"credential-secret")])
def test_mqtt_password_environment_precedes_credential(tmp_path,monkeypatch,environment,expected):
    if environment:
        monkeypatch.setenv("ASR_TEST_PASSWORD",environment)
    else:
        monkeypatch.delenv("ASR_TEST_PASSWORD",raising=False)
    client = Client()
    captured = []
    client.username_pw_set = lambda username,password:captured.append((username,password))
    config = dict(agent_id="asr",model="mock",database=str(tmp_path/"db"),mqtt=dict(host="localhost",tls=False,username="worker",password_env="ASR_TEST_PASSWORD"))
    Worker(config,client=client,password="credential-secret")
    assert captured == [("worker",expected)]


def test_pending_counts_ignore_done_history_and_existing_database_gets_index(tmp_path):
    path = tmp_path/"counts.sqlite"
    store = Store(path)
    # Simulate a database created before the status index existed.
    store.db.execute("DROP INDEX jobs_status")
    store.db.executemany("INSERT INTO jobs(id,digest,status) VALUES(?,?,'done')",[(str(i),str(i)) for i in range(1000)])
    store.db.commit()
    store.db.close()
    store = Store(path)
    assert "jobs_status" in {row[1] for row in store.db.execute("PRAGMA index_list(jobs)")}
    assert store.counts() == dict(queued=0,processing=0,outbox=0)
    items = [fixture_job() for _ in range(3)]
    for item in items:
        store.accept(item)
    first,_ = store.claim()
    store.finish(first["job_id"],dict(status="completed"))
    store.claim()
    assert store.counts() == dict(queued=1,processing=1,outbox=1)
    store.acknowledge(first["job_id"])
    assert store.counts() == dict(queued=1,processing=1,outbox=0)
    assert store.db.execute("SELECT count(*) FROM jobs WHERE status='done'").fetchone()[0] == 1001
    # SQLite searches active status ranges through the covering index, rather
    # than scanning permanent tombstones for every one-second GUI snapshot.
    plans = list(store.db.execute("EXPLAIN QUERY PLAN SELECT status,count(*) FROM jobs WHERE status IN ('queued','processing','outbox') GROUP BY status"))
    assert any("SEARCH jobs USING COVERING INDEX jobs_status" in row[3] for row in plans)

def test_worker_state_reports_durable_queue(tmp_path):
    w,c = worker(tmp_path)
    item = fixture_job()
    deliver(w,c,item)
    assert w.state()['queue'] == dict(queued=1,processing=0,outbox=0,capacity=1)
    assert w.store.claim()
    assert w.state()['queue']['processing'] == 1
    w.store.finish(item['job_id'],dict(job_id=item['job_id'],status='completed'))
    assert w.state()['queue'] == dict(queued=0,processing=0,outbox=1,capacity=1)
    assert 'queue' not in w.state('offline')
    w.store.acknowledge(item['job_id'])
    assert w.state()['queue']['outbox'] == 0
