"""CLI process ownership, line-oriented GUI status, and stdin shutdown."""
import json
import os
from pathlib import Path
import sys
import threading


class DatabaseInUseError(RuntimeError):
    pass


class DatabaseLock:
    """OS-held sidecar lock; never delete the file while another process may open it."""
    def __init__(self, database):
        self.path = Path(str(Path(database).resolve())+".lock")
        self.file = None

    def __enter__(self):
        self.path.parent.mkdir(parents=True,exist_ok=True)
        self.file = self.path.open("a+b")
        try:
            self.file.seek(0,os.SEEK_END)
            if self.file.tell() == 0:
                self.file.write(b"0")
                self.file.flush()
            self.file.seek(0)
            if os.name == "nt":
                import msvcrt
                msvcrt.locking(self.file.fileno(),msvcrt.LK_NBLCK,1)
            else:
                import fcntl
                fcntl.flock(self.file.fileno(),fcntl.LOCK_EX|fcntl.LOCK_NB)
        except OSError:
            self.file.close()
            self.file = None
            raise DatabaseInUseError("worker database is already in use") from None
        return self

    def __exit__(self,*args):
        if self.file is not None:
            if os.name == "nt":
                import msvcrt
                self.file.seek(0)
                msvcrt.locking(self.file.fileno(),msvcrt.LK_UNLCK,1)
            else:
                import fcntl
                fcntl.flock(self.file.fileno(),fcntl.LOCK_UN)
            self.file.close()
            self.file = None


class StatusEvents:
    def __init__(self,stream=None):
        self.stream = stream if stream is not None else sys.stdout
        self.lock = threading.Lock()

    def emit(self,snapshot):
        with self.lock:
            try:
                self.stream.write("ORBIT_STATUS "+json.dumps(snapshot,ensure_ascii=False,separators=(",",":"))+"\n")
                self.stream.flush()
            except (OSError,ValueError):
                # Parent shutdown may close its pipe before the child exits.
                pass


def _windows_pipe_handle(stream):
    """Resolve once before the reader starts; never hold a CRT read lock."""
    if os.name != "nt":
        return None
    import ctypes
    from ctypes import wintypes
    import msvcrt
    try:
        handle = msvcrt.get_osfhandle(stream.fileno())
    except (AttributeError,OSError,ValueError):
        return None  # StringIO and other in-memory test streams
    kernel = ctypes.WinDLL("kernel32",use_last_error=True)
    kernel.GetFileType.argtypes = [wintypes.HANDLE]
    kernel.GetFileType.restype = wintypes.DWORD
    return handle if kernel.GetFileType(handle) == 3 else None  # FILE_TYPE_PIPE


def _poll_control_pipe(stop,peek,read):
    """Never perform an input read until bytes are already available."""
    pending = b""
    try:
        while not stop.is_set():
            available = peek()
            if available is None:
                return  # EOF, broken pipe, or failed control channel.
            if not available:
                stop.wait(.05)
                continue
            data = read(min(available,4096))
            if not data:
                return
            lines = (pending+data).split(b"\n")
            pending = lines.pop()
            if any(line.strip() == b"stop" for line in lines):
                return
            if len(pending)>4096:
                pending = b""
    finally:
        stop.set()


def _read_windows_pipe(stop,handle):
    """Poll a native pipe; do not leave any ReadFile blocked during imports.

    On the target Windows host, even a native blocking ReadFile on stdin can
    prevent NumPy initialization until input arrives. PeekNamedPipe lets the
    control thread sleep without an outstanding read, while still observing
    stop/EOF during model loading. There is exactly one reader for this HANDLE.
    """
    import ctypes
    from ctypes import wintypes
    kernel = ctypes.WinDLL("kernel32",use_last_error=True)
    kernel.PeekNamedPipe.argtypes = [wintypes.HANDLE,wintypes.LPVOID,wintypes.DWORD,
                                    ctypes.POINTER(wintypes.DWORD),ctypes.POINTER(wintypes.DWORD),
                                    ctypes.POINTER(wintypes.DWORD)]
    kernel.PeekNamedPipe.restype = wintypes.BOOL
    kernel.ReadFile.argtypes = [wintypes.HANDLE,wintypes.LPVOID,wintypes.DWORD,
                               ctypes.POINTER(wintypes.DWORD),wintypes.LPVOID]
    kernel.ReadFile.restype = wintypes.BOOL
    buffer = ctypes.create_string_buffer(4096)
    def peek():
        available = wintypes.DWORD()
        if not kernel.PeekNamedPipe(handle,None,0,None,ctypes.byref(available),None):
            return None
        return available.value
    def read(size):
        received = wintypes.DWORD()
        if not kernel.ReadFile(handle,buffer,size,ctypes.byref(received),None):
            return b""
        return buffer.raw[:received.value]
    _poll_control_pipe(stop,peek,read)


def read_control(stop,stream=None):
    stream = stream if stream is not None else sys.stdin
    handle = _windows_pipe_handle(stream)
    def read():
        if handle is not None:
            _read_windows_pipe(stop,handle)
            return
        try:
            for line in stream:
                if line.strip() == "stop":
                    stop.set()
                    return
        except (OSError,ValueError):
            pass
        stop.set()
    thread = threading.Thread(target=read,name="gui-control",daemon=True)
    thread.start()
    return thread
