"""Editable worker settings and system-credential access (no model imports)."""
from __future__ import annotations

from copy import deepcopy
import importlib
import ipaddress
import json
import os
from pathlib import Path
import re
import socket
import tempfile

CREDENTIAL_SERVICE = "orbit-asr-worker"


def _defaults() -> dict:
    return {
        "agent_id": "sdt-asr", "host_label": socket.gethostname(),
        "model": "Qwen/Qwen3-ASR-0.6B", "device": "auto",
        "database": "data/asr.sqlite3", "queue_capacity": 1000,
        "max_attempts": 3, "result_retry_seconds": 5,
        "mqtt": {"host": "", "port": 8883, "tls": True,
                 "username": "", "password_env": "ORBIT_MQTT_PASSWORD"},
    }


def load_config(path: Path) -> dict:
    """Merge editable defaults, without requiring a broker on first launch."""
    defaults = _defaults()
    try:
        raw = json.loads(Path(path).read_text(encoding="utf-8-sig"))
    except FileNotFoundError:
        return defaults
    except (OSError, UnicodeError, ValueError) as exc:
        raise ValueError("无法读取配置，请检查文件是否为有效 JSON，以及是否有读取权限") from exc
    if not isinstance(raw, dict) or ("mqtt" in raw and not isinstance(raw["mqtt"], dict)):
        raise ValueError("配置与 MQTT 配置必须为 JSON 对象")
    defaults.update(deepcopy(raw))
    mqtt = _defaults()["mqtt"]
    mqtt.update(deepcopy(raw.get("mqtt", {})))
    defaults["mqtt"] = mqtt
    return defaults


def _text(value, label: str, maximum: int, allow_empty: bool = False) -> None:
    try:
        valid = (isinstance(value, str) and (allow_empty or bool(value.strip()))
                 and len(value.encode("utf-8")) <= maximum
                 and not any(ord(char) < 32 for char in value))
    except UnicodeError:
        valid = False
    if not valid:
        raise ValueError(f"{label}需为有效文字，最长 {maximum} 字节")


def _integer(value, label: str, low: int, high: int) -> None:
    if type(value) is not int or not low <= value <= high:
        raise ValueError(f"{label}需为 {low}–{high} 之间的整数")


def validate_config(config: dict) -> None:
    if not isinstance(config, dict):
        raise ValueError("配置必须为 JSON 对象")
    agent_id = config.get("agent_id")
    if not isinstance(agent_id, str) or not re.fullmatch(r"[a-z0-9][a-z0-9_-]{0,63}", agent_id):
        raise ValueError("设备 ID 需为 1–64 位小写字母、数字、下划线或短横线，首位为字母或数字")
    _text(config.get("host_label", socket.gethostname()), "本机名称", 256)
    _text(config.get("model"), "模型名称或本地目录", 256)
    if config.get("device", "auto") not in ("auto", "cpu", "cuda"):
        raise ValueError("运行设备只能是 auto、cpu 或 cuda")
    _text(config.get("database", "data/asr.sqlite3"), "队列文件路径", 4096)
    _integer(config.get("queue_capacity", 1000), "队列容量", 1, 100000)
    _integer(config.get("max_attempts", 3), "最大处理次数", 1, 10)
    _integer(config.get("result_retry_seconds", 5), "结果重试间隔", 1, 300)
    mqtt = config.get("mqtt")
    if not isinstance(mqtt, dict):
        raise ValueError("请填写 MQTT 连接配置")
    host = mqtt.get("host")
    _text(host, "MQTT 主机", 253)
    if any(char.isspace() or char in "/?#@[]" for char in host):
        raise ValueError("MQTT 主机只填写域名或 IP，不要包含协议、端口或路径")
    if ":" in host:
        try:
            ipaddress.IPv6Address(host)
        except ValueError as exc:
            raise ValueError("MQTT 主机只填写域名或 IP，端口请单独填写") from exc
    else:
        try:
            domain = host.encode("idna").decode("ascii").rstrip(".")
        except UnicodeError as exc:
            raise ValueError("MQTT 主机需为有效域名或 IP 地址") from exc
        if not domain or any(not re.fullmatch(r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?", label)
                             for label in domain.split(".")):
            raise ValueError("MQTT 主机需为有效域名或 IP 地址")
    _integer(mqtt.get("port", 8883), "MQTT 端口", 1, 65535)
    if type(mqtt.get("tls", True)) is not bool:
        raise ValueError("TLS 选项必须为 true 或 false")
    _text(mqtt.get("username", ""), "MQTT 用户名", 1024, allow_empty=True)
    env = mqtt.get("password_env", "ORBIT_MQTT_PASSWORD")
    if not isinstance(env, str) or not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]{0,254}", env):
        raise ValueError("密码环境变量名需以字母或下划线开头，且仅含字母、数字和下划线")
    for key, label in (("ca_file", "CA 证书路径"), ("cert_file", "客户端证书路径"), ("key_file", "客户端私钥路径")):
        if mqtt.get(key) is not None:
            _text(mqtt[key], label, 4096, allow_empty=True)
    try:
        json.dumps(config, allow_nan=False, ensure_ascii=False).encode("utf-8")
    except (ValueError, TypeError) as exc:
        raise ValueError("配置包含无法保存为 JSON 的值") from exc


def save_config(path: Path, config: dict) -> None:
    """Atomically replace JSON, preserving advanced fields but never a password."""
    validate_config(config)
    value = deepcopy(config)
    value.pop("password", None)
    value["mqtt"].pop("password", None)
    path = Path(path).expanduser()
    temporary = None
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=path.parent,
                                         prefix=f".{path.name}.", suffix=".tmp", delete=False) as stream:
            temporary = Path(stream.name)
            json.dump(value, stream, ensure_ascii=False, indent=2, allow_nan=False)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    except OSError as exc:
        raise ValueError("无法保存配置，请检查目录权限和可用空间") from exc
    finally:
        if temporary is not None:
            try:
                temporary.unlink(missing_ok=True)
            except OSError:
                pass


def _credential_key(path: Path) -> str:
    return str(Path(path).expanduser().resolve())


def _keyring():
    try:
        return importlib.import_module("keyring")
    except ImportError as exc:
        raise ValueError("未安装系统凭据支持，请安装 GUI 依赖") from exc


def get_password(path: Path) -> str | None:
    try:
        return _keyring().get_password(CREDENTIAL_SERVICE, _credential_key(path))
    except Exception as exc:
        raise ValueError("无法读取系统凭据，请检查当前用户的凭据库是否可用") from exc


def save_password(path: Path, password: str) -> None:
    if not isinstance(password, str) or not password:
        raise ValueError("密码不能为空；清除已保存密码请使用删除操作")
    try:
        _keyring().set_password(CREDENTIAL_SERVICE, _credential_key(path), password)
    except Exception as exc:
        raise ValueError("无法保存到系统凭据库，密码尚未保存") from exc


def delete_password(path: Path) -> None:
    try:
        store = _keyring()
        key = _credential_key(path)
        if store.get_password(CREDENTIAL_SERVICE, key) is not None:
            store.delete_password(CREDENTIAL_SERVICE, key)
    except Exception as exc:
        raise ValueError("无法删除系统凭据，请检查当前用户的凭据库是否可用") from exc
