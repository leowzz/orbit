from dataclasses import dataclass
from pathlib import Path
import re
from urllib.parse import quote

import yaml


DEFAULT_BRIGHTNESS_PERCENT = 56
DEFAULT_KEEP_ALIVE_SECONDS = 60
MAX_NODE_ID_BYTES = 64
_NODE_ID_PATTERN = re.compile(r"^[a-z0-9][a-z0-9_-]{0,63}$")


class ConfigError(ValueError):
    pass


@dataclass(frozen=True)
class AppConfig:
    node_id: str
    firmware_version: str
    wifi_ssid: str
    wifi_password: str
    mqtt_host: str
    mqtt_port: int
    mqtt_username: str
    mqtt_password: str
    mqtt_keep_alive_seconds: int
    mqtt_tls_enabled: bool
    mqtt_ca_pem: str
    display_brightness_percent: int


def _mapping(value: object, field: str) -> dict[str, object]:
    if not isinstance(value, dict):
        raise ConfigError(f"{field} must be a mapping")
    return value


def _check_keys(section: dict[str, object], allowed: set[str], field: str) -> None:
    unknown = sorted(set(section) - allowed)
    if unknown:
        raise ConfigError(f"{field} contains unknown keys: {', '.join(unknown)}")


def _text(section: dict[str, object], key: str, field: str) -> str:
    value = section.get(key)
    if not isinstance(value, str) or not value.strip():
        raise ConfigError(f"{field} must be a non-empty string")
    return value


def _integer(section: dict[str, object], key: str, field: str,
            default: int | None = None) -> int:
    value = section.get(key, default)
    if isinstance(value, bool) or not isinstance(value, int):
        raise ConfigError(f"{field} must be an integer")
    return value


def _boolean(section: dict[str, object], key: str, field: str,
             default: bool) -> bool:
    value = section.get(key, default)
    if not isinstance(value, bool):
        raise ConfigError(f"{field} must be a boolean")
    return value


def _node_id(value: str) -> str:
    if len(value.encode("utf-8")) > MAX_NODE_ID_BYTES or not _NODE_ID_PATTERN.fullmatch(value):
        raise ConfigError("node.id must match [a-z0-9][a-z0-9_-]{0,63}")
    return value


def _read_ca(config_path: Path, tls: dict[str, object], enabled: bool) -> str:
    ca_file = tls.get("ca_file", "")
    if not isinstance(ca_file, str):
        raise ConfigError("mqtt.tls.ca_file must be a string")
    if not enabled:
        return ""
    if not ca_file.strip():
        raise ConfigError("mqtt.tls.ca_file is required when TLS is enabled")
    ca_path = Path(ca_file)
    if not ca_path.is_absolute():
        ca_path = config_path.parent / ca_path
    try:
        return ca_path.read_text(encoding="ascii")
    except (OSError, UnicodeError) as error:
        raise ConfigError("mqtt.tls.ca_file could not be read") from error


def load_config(path: Path) -> AppConfig:
    try:
        raw = yaml.safe_load(path.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError) as error:
        raise ConfigError(f"cannot read configuration: {path}") from error

    root = _mapping(raw, "root")
    node = _mapping(root.get("node"), "node")
    wifi = _mapping(root.get("wifi"), "wifi")
    mqtt = _mapping(root.get("mqtt"), "mqtt")
    display_value = root.get("display")
    display = {} if display_value is None else _mapping(display_value, "display")
    tls_value = mqtt.get("tls")
    tls = {} if tls_value is None else _mapping(tls_value, "mqtt.tls")

    _check_keys(root, {"node", "wifi", "mqtt", "display"}, "root")
    _check_keys(node, {"id", "firmware_version"}, "node")
    _check_keys(wifi, {"ssid", "password"}, "wifi")
    _check_keys(
        mqtt,
        {"host", "port", "username", "password", "keep_alive_seconds", "tls"},
        "mqtt",
    )
    _check_keys(tls, {"enabled", "ca_file"}, "mqtt.tls")
    _check_keys(display, {"brightness_percent"}, "display")

    brightness = _integer(
        display, "brightness_percent", "display.brightness_percent",
        DEFAULT_BRIGHTNESS_PERCENT,
    )
    port = _integer(mqtt, "port", "mqtt.port")
    keep_alive = _integer(
        mqtt, "keep_alive_seconds", "mqtt.keep_alive_seconds",
        DEFAULT_KEEP_ALIVE_SECONDS,
    )
    tls_enabled = _boolean(tls, "enabled", "mqtt.tls.enabled", True)
    if not 0 <= brightness <= 100:
        raise ConfigError("display.brightness_percent must be between 0 and 100")
    if not 1 <= port <= 65535:
        raise ConfigError("mqtt.port must be between 1 and 65535")
    if not 10 <= keep_alive <= 300:
        raise ConfigError("mqtt.keep_alive_seconds must be between 10 and 300")

    return AppConfig(
        node_id=_node_id(_text(node, "id", "node.id")),
        firmware_version=_text(node, "firmware_version", "node.firmware_version"),
        wifi_ssid=_text(wifi, "ssid", "wifi.ssid"),
        wifi_password=_text(wifi, "password", "wifi.password"),
        mqtt_host=_text(mqtt, "host", "mqtt.host"),
        mqtt_port=port,
        mqtt_username=_text(mqtt, "username", "mqtt.username"),
        mqtt_password=_text(mqtt, "password", "mqtt.password"),
        mqtt_keep_alive_seconds=keep_alive,
        mqtt_tls_enabled=tls_enabled,
        mqtt_ca_pem=_read_ca(path, tls, tls_enabled),
        display_brightness_percent=brightness,
    )


def _cpp_string(value: str) -> str:
    escaped = (
        value.replace("\\", "\\\\")
        .replace('"', '\\"')
        .replace("\r", "\\r")
        .replace("\n", "\\n")
    )
    return f'"{escaped}"'


def _topic_suffix(node_id: str) -> str:
    return quote(node_id, safe="A-Za-z0-9._-")


def render_header(config: AppConfig) -> str:
    contrast = (config.display_brightness_percent * 255 + 50) // 100
    values = {
        "kNodeId": config.node_id,
        "kFirmwareVersion": config.firmware_version,
        "kWifiSsid": config.wifi_ssid,
        "kWifiPassword": config.wifi_password,
        "kMqttHost": config.mqtt_host,
        "kMqttUsername": config.mqtt_username,
        "kMqttPassword": config.mqtt_password,
        "kMqttCaPem": config.mqtt_ca_pem,
        "kViewTopic": f"orbit/v1/nodes/{_topic_suffix(config.node_id)}/view",
        "kStateTopic": f"orbit/v1/nodes/{_topic_suffix(config.node_id)}/state",
    }
    strings = "\n".join(
        f"inline constexpr char {name}[] = {_cpp_string(value)};"
        for name, value in values.items()
    )
    tls_flag = 1 if config.mqtt_tls_enabled else 0
    return (
        "#pragma once\n\n"
        "#include <stdint.h>\n\n"
        f"#define ORBIT_MQTT_TLS {tls_flag}\n"
        "namespace generated_config {\n"
        f"{strings}\n"
        f"inline constexpr uint16_t kMqttPort = {config.mqtt_port}U;\n"
        f"inline constexpr uint16_t kMqttKeepAliveSeconds = {config.mqtt_keep_alive_seconds}U;\n"
        f"inline constexpr uint8_t kDisplayContrast = {contrast}U;\n"
        "}  // namespace generated_config\n"
    )
