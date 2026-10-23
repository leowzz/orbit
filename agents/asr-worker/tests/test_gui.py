"""Desktop lifecycle checks, without a model, broker, or real credential vault."""
import json
import os
import subprocess
import sys
import time

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

import pytest

pytest.importorskip("PySide6.QtWidgets")
from PySide6.QtCore import QObject, QProcess, Signal
from PySide6.QtTest import QTest
from PySide6.QtWidgets import QApplication, QMessageBox

from orbit_asr import gui
from orbit_asr.controller import WorkerController
from orbit_asr.settings import load_config


@pytest.fixture(scope="module")
def app():
    instance = QApplication.instance() or QApplication([])
    gui.configure_app(instance)
    return instance


class FakeController(QObject):
    changed = Signal(str)
    snapshot = Signal(dict)
    log = Signal(str)
    finished = Signal()

    def __init__(self):
        super().__init__()
        self.active = False
        self.state = "stopped"
        self.starts = []
        self.stops = 0

    def start(self, path, config, password):
        self.starts.append((path, config, password))
        self.active = True
        self.state = "running"
        self.changed.emit(self.state)

    def stop(self):
        self.stops += 1
        self.state = "stopping"
        self.changed.emit(self.state)


@pytest.fixture
def window(app, tmp_path, monkeypatch):
    monkeypatch.setattr(gui, "get_password", lambda _: None)
    monkeypatch.setattr(gui, "save_password", lambda *_: None)
    monkeypatch.setattr(gui, "delete_password", lambda *_: None)
    controller = FakeController()
    result = gui.WorkerWindow(tmp_path / "config.json", controller=controller, tray_available=False)
    yield result
    result.hide()
    result.deleteLater()
    app.processEvents()


def wait_until(condition, timeout=5000):
    deadline = time.monotonic() + timeout / 1000
    while not condition() and time.monotonic() < deadline:
        QTest.qWait(10)
    assert condition()


def test_form_save_start_preserves_advanced_and_masks_password(window, monkeypatch):
    saved = []
    window.config["mqtt"]["cert_file"] = "client.pem"
    window.config["queue_capacity"] = 17
    window.mqtt_host.setText("127.0.0.1")
    window.mqtt_port.setValue(1883)
    window.tls.setChecked(False)
    window.username.setText("worker")
    window.password.setText("session-password")
    window.remember_password.setChecked(True)
    window.device.setCurrentIndex(window.device.findData("cuda"))
    window.model.setCurrentText("D:/模型/qwen")
    monkeypatch.setattr(gui, "save_password", lambda path, password: saved.append((path, password)))
    window.start_worker()
    config = json.loads(window.config_path.read_text(encoding="utf-8"))
    assert "password" not in config["mqtt"]
    assert "session-password" not in window.config_path.read_text(encoding="utf-8")
    assert config["queue_capacity"] == 17 and config["mqtt"]["cert_file"] == "client.pem"
    assert config["device"] == "cuda" and config["model"] == "D:/模型/qwen"
    assert saved == [(window.config_path, "session-password")]
    assert window.controller.starts[0][2] == "session-password"
    assert not window.settings_content.isEnabled()
    assert not window.start_button.isEnabled() and window.stop_button.isEnabled()
    window.start_worker()
    assert len(window.controller.starts) == 1


def test_invalid_form_does_not_start_or_overwrite(window, monkeypatch):
    messages = []
    monkeypatch.setattr(QMessageBox, "warning", lambda *args: messages.append(args[2]))
    window.mqtt_host.setText("mqtt://example.com")
    window.start_worker()
    assert messages and not window.config_path.exists()
    assert not window.controller.starts


def test_empty_password_requires_environment_for_authenticated_broker(window, monkeypatch):
    messages = []
    monkeypatch.setattr(QMessageBox, "warning", lambda *args: messages.append(args[2]))
    monkeypatch.delenv("ORBIT_MQTT_PASSWORD", raising=False)
    window.mqtt_host.setText("localhost")
    window.username.setText("worker")
    window.start_worker()
    assert messages and not window.controller.starts
    monkeypatch.setenv("ORBIT_MQTT_PASSWORD", "environment-password")
    window.start_worker()
    assert len(window.controller.starts) == 1
    assert window.controller.starts[0][2] is None


def test_credential_failure_keeps_session_usable(window, monkeypatch):
    def unavailable(*_):
        raise ValueError("凭据库不可用")
    monkeypatch.setattr(gui, "save_password", unavailable)
    window.mqtt_host.setText("localhost")
    window.username.setText("worker")
    window.password.setText("temporary")
    window.remember_password.setChecked(True)
    window.start_worker()
    assert "系统凭据未更新" in window.notice.text()
    assert window.controller.starts[0][2] == "temporary"


def test_uncheck_remember_deletes_stored_password(window, monkeypatch):
    deleted = []
    window._stored_password = True
    window.mqtt_host.setText("localhost")
    monkeypatch.setattr(gui, "delete_password", lambda path: deleted.append(path))
    assert window.save_settings()
    assert deleted == [window.config_path]
    assert not window._stored_password


def test_status_distinguishes_model_ready_from_broker_and_bounds_logs(window):
    window.controller.active = True
    window.controller.state = "running"
    window.controller.snapshot.emit(dict(status="ready", mqtt_connected=False, subscribed=False,
                                         device="cpu", model="test", queue=dict(queued=2, processing=0, outbox=3)))
    assert window.status_label.text() == "等待 MQTT 连接"
    assert window.mqtt_status.text() == "未连接 · 等待重连"
    assert "待回传 3" in window.queue_status.text()
    window.controller.stop()
    window.controller.snapshot.emit(dict(status="ready", device="cpu"))
    assert window.status_label.text() == "正在停止"
    for _ in range(1505):
        window.controller.log.emit("日志")
    assert window.logs.document().blockCount() == 1500


def test_close_hides_in_tray_without_stopping(window, monkeypatch):
    monkeypatch.setattr(window.tray, "isVisible", lambda: True)
    monkeypatch.setattr(window.tray, "showMessage", lambda *_: None)
    window.controller.active = True
    window.show()
    window.close()
    assert not window.isVisible()
    assert window.controller.stops == 0
    assert not window._quitting


def test_quit_waits_until_child_exits(window, app, monkeypatch):
    exits = []
    monkeypatch.setattr(app, "quit", lambda: exits.append(True))
    window.controller.active = True
    window.quit_app()
    assert window.controller.stops == 1 and not exits
    window.controller.finished.emit()
    assert exits == [True]


def test_bad_native_field_is_validated_before_qt(app, tmp_path):
    path = tmp_path / "config.json"
    config = load_config(path)
    config["mqtt"].update(host="localhost", port=9223372036854775808)
    path.write_text(json.dumps(config))
    with pytest.raises(ValueError, match="端口"):
        gui.WorkerWindow(path, tray_available=False)


def child_program(monkeypatch, controller, source, direct=False):
    original = controller.process.start
    # Fault-injection tests intentionally disable tree termination. Bypass the
    # Windows venv redirector there so test cleanup can own the real process.
    monkeypatch.setattr(controller.process, "start", lambda executable, args: original(
        sys._base_executable if direct and os.name == "nt" else executable,
        ["-u", "-c", source]))


def test_real_process_pipe_start_stop_restart_and_password_redaction(app, tmp_path, monkeypatch):
    controller = WorkerController(stop_timeout_ms=1000)
    source = '''
import json, os, sys, time
print('starting ' + os.environ['ORBIT_MQTT_PASSWORD'][:3], end='', file=sys.stderr, flush=True)
time.sleep(.03)
print(os.environ['ORBIT_MQTT_PASSWORD'][3:], file=sys.stderr, flush=True)
print('ORBIT_STATUS ' + json.dumps(dict(status='ready', mqtt_connected=True, subscribed=True, device='cpu', model='mock', queue=dict(queued=2))), flush=True)
sys.stdin.readline()
print('stopped cleanly', file=sys.stderr, flush=True)
'''
    child_program(monkeypatch, controller, source)
    snapshots, logs, exits = [], [], []
    controller.snapshot.connect(snapshots.append)
    controller.log.connect(logs.append)
    controller.finished.connect(lambda: exits.append(True))
    config = load_config(tmp_path / "config.json")
    for run in range(2):
        controller.start(tmp_path / "config.json", config, "secret-value")
        wait_until(lambda: len(snapshots) == run + 1)
        assert controller.active and snapshots[-1]["queue"]["queued"] == 2
        controller.stop()
        assert controller.state == "stopping"
        controller.start(tmp_path / "config.json", config, "should-not-start")
        wait_until(lambda: len(exits) == run + 1)
        assert controller.state == "stopped" and not controller.active
    assert "secret-value" not in "\n".join(logs)
    assert any("[已隐藏密码]" in line for line in logs)
    assert logs.count("stopped cleanly") == 2


def test_stop_timeout_kills_unresponsive_child(app, tmp_path, monkeypatch):
    controller = WorkerController(stop_timeout_ms=50)
    child_program(monkeypatch, controller, "import time; print('ready', flush=True); time.sleep(60)")
    logs = []
    controller.log.connect(logs.append)
    controller.start(tmp_path / "config.json", load_config(tmp_path / "config.json"))
    wait_until(lambda: "ready" in logs)
    controller.stop()
    wait_until(lambda: not controller.active)
    assert controller.state == "stopped"
    assert any("停止超时" in line for line in logs)


def test_abnormal_exit_and_failed_launch_are_retryable(app, tmp_path, monkeypatch):
    controller = WorkerController()
    child_program(monkeypatch, controller, "raise SystemExit(3)")
    controller.start(tmp_path / "config.json", load_config(tmp_path / "config.json"))
    wait_until(lambda: controller.state == "error")
    assert not controller.active
    original = QProcess.start
    monkeypatch.setattr(controller.process, "start", lambda *_: original(controller.process, str(tmp_path / "no-python"), []))
    controller.start(tmp_path / "config.json", load_config(tmp_path / "config.json"))
    wait_until(lambda: controller.state == "error")
    assert not controller.active


def test_long_unterminated_library_output_is_bounded_and_discarded(app):
    controller = WorkerController()
    logs = []
    controller.log.connect(logs.append)
    controller._consume("stderr", b"x" * 70000)
    assert len(controller._buffers["stderr"]) <= 65536
    controller._consume("stderr", b"private-tail\nnormal\n")
    assert logs == ["normal"]


def test_json_punctuation_password_does_not_break_status_frames(app):
    controller = WorkerController()
    controller._secrets = ['"']
    snapshots = []
    controller.snapshot.connect(snapshots.append)
    controller._consume("stdout", b'ORBIT_STATUS {"status":"ready","model":"test"}\n')
    assert snapshots == [dict(status="ready", model="test")]


def test_desktop_entrypoint_initializes_lock_and_window(tmp_path):
    source = '''
import sys
from PySide6.QtCore import QTimer
from PySide6.QtWidgets import QApplication
from orbit_asr import gui
from shiboken6 import isValid
original_configure = gui.configure_app
observed = []
def request_quit():
    windows = [item for item in QApplication.topLevelWidgets() if isinstance(item, gui.WorkerWindow)]
    assert len(windows) == 1
    observed.extend([windows[0], windows[0].controller, windows[0].tray])
    print('GUI_QUIT_REQUESTED', flush=True)
    windows[0].quit_app()
def configure_with_test_timer(app):
    original_configure(app)
    QTimer.singleShot(100, request_quit)
gui.configure_app = configure_with_test_timer
sys.argv = ['orbit-asr-gui', '--config', sys.argv[1]]
result = gui.main()
assert QApplication.instance() is None
assert observed and all(not isValid(item) for item in observed)
assert not __import__('pathlib').Path(sys.argv[2] + '.gui.lock').exists()
print('GUI_NATIVE_OBJECTS_DISPOSED', flush=True)
raise SystemExit(result)
'''
    environment = os.environ.copy()
    if sys.platform == "win32":
        # Exercise the real Windows desktop/font backend. The offscreen plugin
        # can crash in font teardown even when the native GUI lifecycle works.
        environment["QT_QPA_PLATFORM"] = "windows"
    result = subprocess.run([sys.executable, "-c", source, str(tmp_path / "first-run.json")],
                            capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=15,
                            env=environment)
    assert result.returncode == 0, result.stderr
    assert "GUI_QUIT_REQUESTED" in result.stdout
    assert "GUI_NATIVE_OBJECTS_DISPOSED" in result.stdout


def test_tree_termination_does_not_target_an_unowned_pid(app, tmp_path, monkeypatch):
    controller = WorkerController()
    child_program(monkeypatch, controller, "import time; print('ready', flush=True); time.sleep(60)", direct=True)
    logs, pinned = [], []
    controller.log.connect(logs.append)
    controller._windows = True
    monkeypatch.setattr(controller, "_pin_owned_process", lambda pid: pinned.append(pid))
    controller.start(tmp_path / "config.json", load_config(tmp_path / "config.json"))
    try:
        wait_until(lambda: "ready" in logs)
        controller._owned_pid += 1
        controller._kill()
        assert not pinned
        assert controller._terminator.state() == QProcess.ProcessState.NotRunning
        assert any("身份" in line for line in logs)
    finally:
        controller.process.kill()
        wait_until(lambda: not controller.active)


def test_tree_helper_launch_failure_is_visible_and_does_not_kill_only_wrapper(app, tmp_path, monkeypatch):
    controller = WorkerController(stop_timeout_ms=50)
    child_program(monkeypatch, controller, "import time; print('ready', flush=True); time.sleep(60)", direct=True)
    logs, exits = [], []
    controller.log.connect(logs.append)
    controller.finished.connect(lambda: exits.append(True))
    controller._windows = True
    monkeypatch.setattr(controller, "_pin_owned_process", lambda pid: True)
    monkeypatch.setattr(controller, "_taskkill_executable", lambda: str(tmp_path / "missing-taskkill.exe"))
    controller.start(tmp_path / "config.json", load_config(tmp_path / "config.json"))
    try:
        wait_until(lambda: "ready" in logs)
        controller.stop()
        wait_until(lambda: controller.state == "error")
        assert controller.process.state() == QProcess.ProcessState.Running
        assert not exits and controller._owned_handle is None
        assert not controller._terminator_timer.isActive()
        assert any("整个转写进程树" in line for line in logs)
        assert not controller._stop_requested
    finally:
        controller.process.kill()
        wait_until(lambda: not controller.active)


@pytest.mark.skipif(os.name != "nt", reason="Windows taskkill tree and native process-handle regression")
def test_windows_timeout_terminates_real_child_and_grandchild(app, tmp_path, monkeypatch):
    import ctypes
    from ctypes import wintypes
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
    kernel.OpenProcess.restype = wintypes.HANDLE
    kernel.WaitForSingleObject.argtypes = [wintypes.HANDLE, wintypes.DWORD]
    kernel.WaitForSingleObject.restype = wintypes.DWORD
    kernel.TerminateProcess.argtypes = [wintypes.HANDLE, wintypes.UINT]
    kernel.TerminateProcess.restype = wintypes.BOOL
    kernel.CloseHandle.argtypes = [wintypes.HANDLE]
    kernel.CloseHandle.restype = wintypes.BOOL
    grandchild = "import os,time; print('grandchild_pid:' + str(os.getpid()), flush=True); time.sleep(60)"
    child = ("import os,subprocess,sys,time; "
             f"subprocess.Popen([sys.executable, '-u', '-c', {grandchild!r}]); "
             "print('child_pid:' + str(os.getpid()), flush=True); time.sleep(60)")
    source = ("import os,subprocess,sys,time; "
              f"subprocess.Popen([sys.executable, '-u', '-c', {child!r}]); "
              "print('parent_pid:' + str(os.getpid()), flush=True); time.sleep(60)")
    controller = WorkerController(stop_timeout_ms=50)
    child_program(monkeypatch, controller, source)
    pids, handles, exits = {}, [], []
    def collect(line):
        if "_pid:" in line:
            label, number = line.split(":", 1)
            pids[label] = int(number)
    controller.log.connect(collect)
    controller.finished.connect(lambda: exits.append(True))
    controller.start(tmp_path / "config.json", load_config(tmp_path / "config.json"))
    try:
        wait_until(lambda: len(pids) == 3, timeout=15000)
        for pid in pids.values():
            handle = kernel.OpenProcess(0x00100001, False, pid)  # SYNCHRONIZE | TERMINATE (cleanup only)
            assert handle, ctypes.get_last_error()
            handles.append(handle)
            assert kernel.WaitForSingleObject(handle, 0) == 0x102
        controller.stop()
        # The original Qt wrapper exiting is insufficient: inspect real handles
        # for the Python worker, its child and its child's child independently.
        wait_until(lambda: all(kernel.WaitForSingleObject(handle, 0) == 0 for handle in handles), timeout=15000)
        wait_until(lambda: bool(exits) and not controller.active, timeout=15000)
        assert controller.state == "stopped"
        assert controller._terminator.state() == QProcess.ProcessState.NotRunning
        assert controller._owned_handle is None
    finally:
        for handle in handles:
            if kernel.WaitForSingleObject(handle, 0) == 0x102:
                kernel.TerminateProcess(handle, 1)
            kernel.CloseHandle(handle)
        if controller.process.state() != QProcess.ProcessState.NotRunning:
            controller.process.kill()
            controller.process.waitForFinished(5000)
        if controller._terminator.state() != QProcess.ProcessState.NotRunning:
            controller._terminator.kill()
            controller._terminator.waitForFinished(5000)


def test_controller_stays_active_until_tree_helper_finishes(app, tmp_path, monkeypatch):
    controller = WorkerController()
    exits = []
    controller.finished.connect(lambda: exits.append(True))
    monkeypatch.setattr(controller, "_stdout", lambda: None)
    monkeypatch.setattr(controller, "_stderr", lambda: None)
    controller._stop_requested = True
    controller._set_state("stopping")
    controller._terminator.start(sys.executable, ["-c", "import time; time.sleep(.3)"])
    wait_until(lambda: controller._terminator.state() == QProcess.ProcessState.Running)
    controller._finished(0, QProcess.ExitStatus.NormalExit)
    assert controller.active and controller._pending_finish is not None and not exits
    controller.start(tmp_path / "config.json", load_config(tmp_path / "config.json"))
    assert controller.process.state() == QProcess.ProcessState.NotRunning
    wait_until(lambda: bool(exits))
    assert not controller.active and controller._pending_finish is None
    assert controller.state == "stopped"


def test_synchronous_tree_helper_failure_does_not_leave_timeout_armed(app, tmp_path, monkeypatch):
    controller = WorkerController()
    controller._windows = True
    controller._owned_pid = 123
    controller._stop_requested = True
    monkeypatch.setattr(controller.process, "state", lambda: QProcess.ProcessState.Running)
    monkeypatch.setattr(controller.process, "processId", lambda: 123)
    monkeypatch.setattr(controller, "_pin_owned_process", lambda pid: True)
    monkeypatch.setattr(controller, "_taskkill_executable", lambda: str(tmp_path / "missing-taskkill.exe"))
    monkeypatch.setattr(controller._terminator, "start", lambda *_: controller._terminator.errorOccurred.emit(QProcess.ProcessError.FailedToStart))
    controller._kill()
    assert controller.state == "error"
    assert not controller._terminator_timer.isActive()
    assert not controller._stop_requested
