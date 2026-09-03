from pathlib import Path

import pytest

from scripts.config_codegen import ConfigError, load_config, render_header


VALID = """
node:
  id: desk-oled-01
  firmware_version: 0.1.0
wifi:
  ssid: lab-wifi
  password: wifi-secret
mqtt:
  host: broker.example.com
  port: 8883
  username: node-user
  password: mqtt-secret
  tls:
    enabled: false
    ca_file: ""
display:
  brightness_percent: 56
"""


def write_config(tmp_path: Path, text: str) -> Path:
    path = tmp_path / "config.yaml"
    path.write_text(text, encoding="utf-8")
    return path


def test_valid_config_generates_topics_and_contrast(tmp_path: Path) -> None:
    config = load_config(write_config(tmp_path, VALID))
    header = render_header(config)

    assert 'kNodeId[] = "desk-oled-01"' in header
    assert 'kViewTopic[] = "orbit/v1/nodes/desk-oled-01/view"' in header
    assert 'kStateTopic[] = "orbit/v1/nodes/desk-oled-01/state"' in header
    assert "kDisplayContrast = 143U" in header
    assert "ORBIT_MQTT_TLS 0" in header


@pytest.mark.parametrize("node_id", ["Desk-oled-01", "desk.oled-01", "_desk-oled-01", "desk-oled-01!"])
def test_rejects_noncanonical_node_id(tmp_path: Path, node_id: str) -> None:
    text = VALID.replace("id: desk-oled-01", f"id: {node_id}")
    with pytest.raises(ConfigError, match="node.id"):
        load_config(write_config(tmp_path, text))


@pytest.mark.parametrize(
    ("old", "new", "field"),
    [
        ("id: desk-oled-01", "id: ''", "node.id"),
        ("ssid: lab-wifi", "ssid: ''", "wifi.ssid"),
        ("password: wifi-secret", "password: ''", "wifi.password"),
        ("username: node-user", "username: ''", "mqtt.username"),
        ("password: mqtt-secret", "password: ''", "mqtt.password"),
    ],
)
def test_rejects_empty_required_fields(
    tmp_path: Path, old: str, new: str, field: str
) -> None:
    with pytest.raises(ConfigError, match=field):
        load_config(write_config(tmp_path, VALID.replace(old, new)))


@pytest.mark.parametrize("brightness", ["-1", "101", "false", "50.0"])
def test_rejects_invalid_brightness(tmp_path: Path, brightness: str) -> None:
    text = VALID.replace("brightness_percent: 56", f"brightness_percent: {brightness}")
    with pytest.raises(ConfigError, match="display.brightness_percent"):
        load_config(write_config(tmp_path, text))


def test_rejects_invalid_port_without_echoing_password(tmp_path: Path) -> None:
    text = VALID.replace("port: 8883", "port: 0")
    with pytest.raises(ConfigError) as caught:
        load_config(write_config(tmp_path, text))
    assert "mqtt.port" in str(caught.value)
    assert "mqtt-secret" not in str(caught.value)


def test_tls_requires_a_readable_ca_file(tmp_path: Path) -> None:
    text = VALID.replace("enabled: false", "enabled: true")
    with pytest.raises(ConfigError, match="ca_file"):
        load_config(write_config(tmp_path, text))


def test_rejects_unknown_keys(tmp_path: Path) -> None:
    text = VALID.replace("  port: 8883", "  port: 8883\n  api_token: do-not-accept")
    with pytest.raises(ConfigError, match="mqtt contains unknown keys: api_token"):
        load_config(write_config(tmp_path, text))
