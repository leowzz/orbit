"""Qt process control. Inference never runs on the desktop event loop."""

import json
import os
from pathlib import Path
import sys

from PySide6.QtCore import QObject, QProcess, QProcessEnvironment, QTimer, Signal


class WorkerController(QObject):
    changed = Signal(str)
    snapshot = Signal(dict)
    log = Signal(str)
    finished = Signal()

    def __init__(self, parent=None, stop_timeout_ms=10000):
        super().__init__(parent)
        self.process = QProcess(self)
        self.process.setProcessChannelMode(QProcess.ProcessChannelMode.SeparateChannels)
        self.process.started.connect(self._started)
        self.process.readyReadStandardOutput.connect(self._stdout)
        self.process.readyReadStandardError.connect(self._stderr)
        self.process.finished.connect(self._finished)
        self.process.errorOccurred.connect(self._error)
        self.timer = QTimer(self)
        self.timer.setSingleShot(True)
        self.timer.setInterval(stop_timeout_ms)
        self.timer.timeout.connect(self._kill)
        self.state = "stopped"
        self._buffers = {"stdout": b"", "stderr": b""}
        self._discarding = set()
        self._secrets = []
        self._stop_requested = False
        self._windows = os.name == "nt"
        self._owned_pid = 0
        self._owned_handle = None
        self._pending_finish = None
        self._tree_failure = False
        self._terminator = QProcess(self)
        self._terminator.setStandardOutputFile(QProcess.nullDevice())
        self._terminator.setStandardErrorFile(QProcess.nullDevice())
        self._terminator.finished.connect(self._tree_finished)
        self._terminator.errorOccurred.connect(self._tree_error)
        self._terminator_timer = QTimer(self)
        self._terminator_timer.setSingleShot(True)
        self._terminator_timer.setInterval(10000)
        self._terminator_timer.timeout.connect(self._tree_timeout)

    @property
    def active(self):
        return (self.process.state() != QProcess.ProcessState.NotRunning
                or self._terminator.state() != QProcess.ProcessState.NotRunning)

    def _set_state(self, state):
        self.state = state
        self.changed.emit(state)

    def start(self, config_path, config, password=None):
        if self.active:
            return
        path = Path(config_path).resolve()
        env = QProcessEnvironment.systemEnvironment()
        env.insert("PYTHONUNBUFFERED", "1")
        env.insert("PYTHONIOENCODING", "utf-8")
        password_env = config["mqtt"].get("password_env", "ORBIT_MQTT_PASSWORD")
        if password is not None:
            env.insert(password_env, password)
        self._secrets = [value for value in (password, env.value(password_env)) if value]
        self.process.setProcessEnvironment(env)
        self.process.setWorkingDirectory(str(path.parent))
        executable = Path(sys.executable)
        # A pythonw GUI still needs a console-capable child with redirected pipes.
        if os.name == "nt" and executable.name.lower() == "pythonw.exe":
            executable = executable.with_name("python.exe")
        self._buffers = {"stdout": b"", "stderr": b""}
        self._discarding.clear()
        self._stop_requested = False
        self._owned_pid = 0
        self._pending_finish = None
        self._tree_failure = False
        self._set_state("starting")
        self.log.emit("正在启动转写进程…")
        self.process.start(str(executable), ["-u", "-m", "orbit_asr.worker", "--config", str(path),
                                             "--gui-events", "--control-stdin"])

    def _started(self):
        self._owned_pid = int(self.process.processId())
        if self._stop_requested:
            self._send_stop()
        else:
            self._set_state("running")

    def stop(self):
        if not self.active or self._stop_requested:
            return
        self._stop_requested = True
        self._set_state("stopping")
        self.log.emit("正在停止；未完成任务会保留到下次启动。")
        self.timer.start()
        if self.process.state() == QProcess.ProcessState.Running:
            self._send_stop()

    def _send_stop(self):
        self.process.write(b"stop\n")
        self.process.closeWriteChannel()

    def _kill(self):
        if self.process.state() == QProcess.ProcessState.NotRunning:
            return
        if not self._windows:
            self.log.emit("进程停止超时，正在结束进程；待处理任务已保存在本机。")
            self.process.kill()
            return
        if self._terminator.state() != QProcess.ProcessState.NotRunning:
            return
        # Only target the PID captured from this QProcess.started. Never look up
        # children by name or reuse a stale PID after its process has finished.
        pid = self._owned_pid
        if not pid or int(self.process.processId()) != pid:
            self.log.emit("尚未确认转写进程身份，未执行强制结束。")
            return
        try:
            if not self._pin_owned_process(pid):
                return  # The original process already exited; let Qt deliver finished.
            executable = self._taskkill_executable()
        except OSError:
            self._release_owned_process()
            self._tree_failed()
            return
        self.log.emit("进程停止超时，正在结束转写进程及其子进程；待处理任务已保存在本机。")
        # FailedToStart can be delivered synchronously inside start() on Windows.
        # Arm first so the error handler always leaves this timer stopped.
        self._terminator_timer.start()
        self._terminator.start(executable, ["/PID", str(pid), "/T", "/F"])

    def _pin_owned_process(self, pid):
        import ctypes
        from ctypes import wintypes
        kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        kernel.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
        kernel.OpenProcess.restype = wintypes.HANDLE
        kernel.WaitForSingleObject.argtypes = [wintypes.HANDLE, wintypes.DWORD]
        kernel.WaitForSingleObject.restype = wintypes.DWORD
        handle = kernel.OpenProcess(0x00100000, False, pid)  # SYNCHRONIZE
        if not handle:
            raise ctypes.WinError(ctypes.get_last_error())
        self._owned_handle = handle
        # Retain the original process object until taskkill completes, preventing
        # a terminated process's identity from being recycled underneath the helper.
        wait_status = kernel.WaitForSingleObject(handle, 0)
        if wait_status == 0xFFFFFFFF:  # WAIT_FAILED
            error = ctypes.WinError(ctypes.get_last_error())
            self._release_owned_process()
            raise error
        if wait_status != 0x102:  # WAIT_TIMEOUT = alive
            self._release_owned_process()
            return False
        return True

    @staticmethod
    def _taskkill_executable():
        import ctypes
        from ctypes import wintypes
        kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        kernel.GetSystemDirectoryW.argtypes = [wintypes.LPWSTR, wintypes.UINT]
        kernel.GetSystemDirectoryW.restype = wintypes.UINT
        buffer = ctypes.create_unicode_buffer(32768)
        length = kernel.GetSystemDirectoryW(buffer, len(buffer))
        if not 0 < length < len(buffer):
            raise ctypes.WinError(ctypes.get_last_error())
        return str(Path(buffer.value) / "taskkill.exe")

    def _release_owned_process(self):
        if self._owned_handle is not None:
            import ctypes
            from ctypes import wintypes
            kernel = ctypes.WinDLL("kernel32", use_last_error=True)
            kernel.CloseHandle.argtypes = [wintypes.HANDLE]
            kernel.CloseHandle.restype = wintypes.BOOL
            kernel.CloseHandle(self._owned_handle)
            self._owned_handle = None

    def _tree_failed(self):
        self._tree_failure = True
        self.log.emit("未能结束整个转写进程树，请检查系统权限后重试；请勿启动另一个实例。")
        self._set_state("error")
        if self.process.state() != QProcess.ProcessState.NotRunning:
            self._stop_requested = False  # Allow another explicit stop attempt.

    def _tree_timeout(self):
        self._tree_failed()
        self._terminator.kill()  # Only the helper; never abandon a live helper on quit.

    def _tree_error(self, error):
        if error == QProcess.ProcessError.FailedToStart:
            self._terminator_timer.stop()
            self._release_owned_process()
            self._tree_failed()
            self._finish_pending()

    def _tree_finished(self, code, exit_status):
        self._terminator_timer.stop()
        self._release_owned_process()
        if code != 0 or exit_status != QProcess.ExitStatus.NormalExit:
            self._tree_failed()
        self._finish_pending()

    def _finish_pending(self):
        if self._pending_finish is not None:
            result = self._pending_finish
            self._pending_finish = None
            self._complete_finished(*result)

    def _stdout(self):
        self._consume("stdout", bytes(self.process.readAllStandardOutput()))

    def _stderr(self):
        self._consume("stderr", bytes(self.process.readAllStandardError()))

    def _consume(self, channel, data, final=False):
        # Bound a library's unterminated output as well as the visible log. Do not
        # emit fragments: a password can span any two pipe reads.
        data = self._buffers[channel] + data
        lines = data.split(b"\n")
        tail = lines.pop()
        for line in lines:
            if channel in self._discarding:
                self._discarding.remove(channel)
            elif len(line) <= 65536:
                self._line(channel, line)
            else:
                self.log.emit("已省略过长的日志行。")
        if len(tail) > 65536:
            self._discarding.add(channel)
            tail = b""
        if final:
            if channel not in self._discarding and tail:
                self._line(channel, tail)
            tail = b""
            self._discarding.discard(channel)
        self._buffers[channel] = tail

    def _line(self, channel, line):
        text = line.decode("utf-8", errors="replace").rstrip("\r")
        if channel == "stdout" and text.startswith("ORBIT_STATUS "):
            try:
                payload = json.loads(text[len("ORBIT_STATUS "):])
                if isinstance(payload, dict):
                    self.snapshot.emit(self._redact(payload))
                    return
            except (ValueError, TypeError):
                pass
            self.log.emit("未能读取运行状态。")
        elif text:
            self.log.emit(self._redact(text))

    def _redact(self, value):
        # Parse status frames before redaction: a password may itself contain
        # JSON punctuation. Replacing it in the wire bytes would corrupt IPC.
        if isinstance(value, str):
            for secret in set(self._secrets):
                value = value.replace(secret, "[已隐藏密码]")
        elif isinstance(value, dict):
            value = {key: self._redact(item) for key, item in value.items()}
        elif isinstance(value, list):
            value = [self._redact(item) for item in value]
        return value

    def _finished(self, code, exit_status):
        self.timer.stop()
        if self._terminator.state() != QProcess.ProcessState.NotRunning:
            self._pending_finish = (code, exit_status)
            return
        self._complete_finished(code, exit_status)

    def _complete_finished(self, code, exit_status):
        self.timer.stop()
        self._owned_pid = 0
        self._release_owned_process()
        self._stdout()
        self._stderr()
        for channel in self._buffers:
            self._consume(channel, b"", final=True)
        stopped = not self._tree_failure and (self._stop_requested or (code == 0 and exit_status == QProcess.ExitStatus.NormalExit))
        self._set_state("stopped" if stopped else "error")
        self.log.emit("转写进程已停止。" if stopped else f"转写进程异常退出（{code}），请查看上方日志。")
        self._secrets.clear()
        self.process.setProcessEnvironment(QProcessEnvironment())
        self.finished.emit()

    def _error(self, error):
        if error == QProcess.ProcessError.FailedToStart:
            self.timer.stop()
            self.log.emit("无法启动 Python 进程，请检查 worker 的安装环境。")
            self._set_state("error")
            self._secrets.clear()
            self.process.setProcessEnvironment(QProcessEnvironment())
            self.finished.emit()
