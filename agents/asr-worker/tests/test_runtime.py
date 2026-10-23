import io
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import time
from types import SimpleNamespace

import pytest

from orbit_asr.runtime import DatabaseLock,StatusEvents,read_control
from test_worker import worker,fixture_job,deliver


def test_database_lock_excludes_second_process_and_releases(tmp_path):
    path = str(tmp_path/"queue.sqlite")
    code = "from orbit_asr.runtime import DatabaseLock; import sys; lock=DatabaseLock(sys.argv[1]); lock.__enter__()"
    with DatabaseLock(path):
        result = subprocess.run([sys.executable,"-c",code,path],capture_output=True,text=True)
        assert result.returncode != 0
        assert "already in use" in result.stderr
        assert not Path(path).exists()  # lock precedes any SQLite open/reset
    result = subprocess.run([sys.executable,"-c",code,path],capture_output=True,text=True)
    assert result.returncode == 0
    # OS releases the lock even when a process ends without __exit__.
    with DatabaseLock(path):
        pass


@pytest.mark.parametrize("content",["stop\n", "unknown\n", ""])
def test_control_stop_or_eof(content):
    stop = threading.Event()
    thread = read_control(stop,io.StringIO(content))
    thread.join(1)
    assert stop.is_set()
    assert thread.daemon


def test_gui_snapshots_connection_subscription_queue_and_stop(tmp_path):
    w,c = worker(tmp_path)
    stream = io.StringIO()
    w.events = StatusEvents(stream)
    w.emit_status()
    w.on_connect(c,None,None,SimpleNamespace(is_failure=False),None)
    assert w.snapshot()["mqtt_connected"]
    assert not w.snapshot()["subscribed"]
    w.on_subscribe(c,None,1,[SimpleNamespace(is_failure=False)],None)
    assert w.snapshot()["subscribed"]
    deliver(w,c,fixture_job())
    assert w.snapshot()["queue"]["queued"] == 1
    w.status = "ready"
    w.on_disconnect(c,None,None,None,None)
    assert not w.snapshot()["mqtt_connected"]
    assert not w.snapshot()["subscribed"]
    assert w.snapshot()["status"] == "ready"  # independent model vs network state
    w.stop.set()
    class Model:
        def transcribe(self,audio): pytest.fail("must not claim after stop")
    assert not w.process_one(Model())
    assert w.snapshot()["queue"]["queued"] == 1
    w.status = "offline"
    w.status = "ready"  # late model thread cannot overwrite terminal status
    snapshots = [json.loads(line.removeprefix("ORBIT_STATUS ")) for line in stream.getvalue().splitlines()]
    assert snapshots[0]["status"] == "starting"
    assert snapshots[-1]["status"] == "offline"
    assert set(snapshots[-1]) == {"status","mqtt_connected","subscribed","device","model","queue"}


def test_cli_failure_is_framed_and_stderr_only(tmp_path):
    result = subprocess.run([sys.executable,"-m","orbit_asr.worker","--config",str(tmp_path/"missing.json"),"--gui-events","--control-stdin"],input="stop\n",capture_output=True,text=True)
    assert result.returncode == 1
    lines = result.stdout.splitlines()
    assert len(lines) == 2
    assert all(line.startswith("ORBIT_STATUS ") for line in lines)
    assert json.loads(lines[0][13:])["status"] == "starting"
    assert json.loads(lines[-1][13:])["status"] == "error"
    assert "FileNotFoundError" in result.stderr


def test_cli_locked_database_never_opens_store(tmp_path):
    path = tmp_path/"queue.sqlite"
    config = tmp_path/"config.json"
    config.write_text(json.dumps(dict(agent_id="asr",model="mock",database=str(path),mqtt=dict(host="localhost",tls=False))))
    with DatabaseLock(path):
        result = subprocess.run([sys.executable,"-m","orbit_asr.worker","--config",str(config),"--gui-events"],capture_output=True,text=True)
    assert result.returncode == 1
    assert not path.exists()
    assert json.loads(result.stdout.splitlines()[-1][13:])["status"] == "error"


def test_run_emits_heartbeat_and_terminal_offline_without_model_download(tmp_path,monkeypatch):
    import orbit_asr.worker as module
    w,c = worker(tmp_path)
    monkeypatch.setattr(module,"resolve_device",lambda device:"cpu")
    class Model:
        device = "cpu"
        def __init__(self,*args): pass
        def transcribe(self,audio): return "unused","English"
    w.model_factory = Model
    stream = io.StringIO()
    w.events = StatusEvents(stream)
    c.connect_async = lambda *args,**kwargs: None
    def loop_start():
        w.on_connect(c,None,None,SimpleNamespace(is_failure=False),None)
        w.on_subscribe(c,None,1,[SimpleNamespace(is_failure=False)],None)
    c.loop_start = loop_start
    c.loop_stop = lambda: None
    c.disconnect = lambda:w.on_disconnect(c,None,None,None,None)
    original_publish = c.publish
    def publish(*args,**kwargs):
        original_publish(*args,**kwargs)
        return SimpleNamespace(wait_for_publish=lambda **kwargs:None)
    c.publish = publish
    thread = threading.Thread(target=w.run)
    thread.start()
    deadline = time.monotonic()+3
    while w.status != "ready" and time.monotonic()<deadline:
        time.sleep(.01)
    assert w.status == "ready"
    count = len(stream.getvalue().splitlines())
    time.sleep(1.1)
    assert len(stream.getvalue().splitlines())>count
    w.stop.set()
    thread.join(3)
    assert not thread.is_alive()
    snapshot = json.loads(stream.getvalue().splitlines()[-1][13:])
    assert snapshot["status"] == "offline"
    assert snapshot["mqtt_connected"] is False
    assert snapshot["subscribed"] is False


def test_device_unknown_until_resolved_and_wire_remains_valid(tmp_path):
    w,c = worker(tmp_path)
    assert w.snapshot()["device"] is None
    assert w.snapshot()["model"] == "mock"
    assert w.state()["device"] in ("cpu","cuda")
    w.device = "cuda"
    w.device_resolved = True
    assert w.snapshot()["device"] == "cuda"


@pytest.mark.parametrize("exception,stage,device,expected",[
    (ModuleNotFoundError("private-secret",name="torch"),"device","auto","PyTorch"),
    (ModuleNotFoundError("private-secret",name="qwen_asr"),"model","cpu","qwen-asr"),
    (RuntimeError("private-secret"),"device","cuda","CUDA 不可用"),
    (OSError("private-secret"),"model","cpu","模型加载失败"),
])
def test_safe_actionable_startup_diagnostics(caplog,exception,stage,device,expected):
    from orbit_asr.worker import log_startup_failure
    log_startup_failure(exception,stage,device)
    assert expected in caplog.text
    assert "private-secret" not in caplog.text


def test_failed_device_resolution_never_reports_cpu(tmp_path,monkeypatch,caplog):
    import orbit_asr.worker as module
    w,c = worker(tmp_path)
    stream = io.StringIO()
    w.events = StatusEvents(stream)
    w.config["device"] = "cuda"
    def fail(device): raise RuntimeError("private-secret")
    monkeypatch.setattr(module,"resolve_device",fail)
    w.run()
    snapshots = [json.loads(line[13:]) for line in stream.getvalue().splitlines()]
    assert all(snapshot["device"] is None for snapshot in snapshots)
    assert snapshots[-1]["status"] == "error"
    assert "CUDA 不可用" in caplog.text
    assert "private-secret" not in caplog.text


def test_model_load_failure_has_safe_guidance(tmp_path,caplog):
    w,c = worker(tmp_path)
    def fail(*args): raise OSError("private-secret")
    w.model_factory = fail
    w.process_loop()
    assert w.fatal
    assert w.status == "error"
    assert "模型加载失败" in caplog.text
    assert "private-secret" not in caplog.text


@pytest.mark.skipif(os.name != "nt",reason="regression requires Windows CRT and native pipes")
@pytest.mark.parametrize("command",["stop", "eof"])
def test_windows_control_pipe_does_not_block_numpy_import(command):
    import queue
    code = """
import sys, threading
from orbit_asr.runtime import read_control
stop = threading.Event()
read_control(stop)
import numpy
print('ready', flush=True)
assert stop.wait(10), 'control command did not stop child'
print('stopped', flush=True)
"""
    # stdin intentionally stays open until NumPy has imported and emitted ready.
    # The old TextIOWrapper/CRT reader blocks NumPy's initialization here.
    child = subprocess.Popen([sys.executable,"-c",code],stdin=subprocess.PIPE,
                             stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
    lines = queue.Queue()
    def read_output():
        for line in child.stdout:
            lines.put(line.strip())
    reader = threading.Thread(target=read_output,daemon=True)
    reader.start()
    try:
        assert lines.get(timeout=20) == "ready"
        if command == "stop":
            child.stdin.write("stop\n")
            child.stdin.flush()
        else:
            child.stdin.close()
        assert lines.get(timeout=10) == "stopped"
        assert child.wait(timeout=10) == 0
    finally:
        if child.poll() is None:
            # venv python.exe can be a redirector; clean up its whole child tree.
            subprocess.run(["taskkill","/PID",str(child.pid),"/T","/F"],capture_output=True)
            child.wait(timeout=10)
        if not child.stdin.closed:
            child.stdin.close()
        child.stdout.close()
        child.stderr.close()


def test_control_poll_does_not_read_empty_pipe_and_preserves_fragmented_stop():
    from orbit_asr.runtime import _poll_control_pipe
    stop = threading.Event()
    available = iter([0,0,2,3])
    chunks = iter([b"st",b"op\n"])
    reads = []
    def peek(): return next(available)
    def read(size):
        reads.append(size)
        return next(chunks)
    _poll_control_pipe(stop,peek,read)
    assert reads == [2,3]
    assert stop.is_set()


def test_control_poll_eof_never_reads():
    from orbit_asr.runtime import _poll_control_pipe
    stop = threading.Event()
    def unexpected(size): pytest.fail("read called without available bytes")
    _poll_control_pipe(stop,lambda:None,unexpected)
    assert stop.is_set()


def test_control_poll_observes_external_stop_while_pipe_open():
    from orbit_asr.runtime import _poll_control_pipe
    stop = threading.Event()
    def unexpected(size): pytest.fail("read called without available bytes")
    thread = threading.Thread(target=_poll_control_pipe,args=(stop,lambda:0,unexpected))
    thread.start()
    stop.set()
    thread.join(1)
    assert not thread.is_alive()
