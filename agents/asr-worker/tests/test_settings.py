from copy import deepcopy
import json
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock

import pytest
from orbit_asr import settings


def configured(tmp_path):
    value = settings.load_config(tmp_path / "missing.json")
    value["mqtt"]["host"] = "broker.example.internal"
    return value


def test_missing_configuration_is_editable_and_defaults_are_independent(tmp_path):
    first = settings.load_config(tmp_path / "config.json")
    assert first["agent_id"] == "sdt-asr"
    assert first["model"] == "Qwen/Qwen3-ASR-0.6B"
    assert first["mqtt"] == {"host": "", "port": 8883, "tls": True,
                             "username": "", "password_env": "ORBIT_MQTT_PASSWORD"}
    first["mqtt"]["host"] = "changed"
    assert settings.load_config(tmp_path / "config.json")["mqtt"]["host"] == ""
    with pytest.raises(ValueError, match="MQTT"):
        settings.validate_config(settings.load_config(tmp_path / "config.json"))


def test_load_merges_nested_defaults_and_preserves_advanced_keys(tmp_path):
    path = tmp_path / "config.json"
    path.write_text(json.dumps({"mqtt": {"host": "localhost", "ca_file": "C:/CA.pem"},
                                "advanced": {"languages": ["Chinese"]}}), encoding="utf-8-sig")
    value = settings.load_config(path)
    assert value["mqtt"]["tls"] is True
    assert value["mqtt"]["ca_file"] == "C:/CA.pem"
    assert value["advanced"]["languages"] == ["Chinese"]
    settings.validate_config(value)


@pytest.mark.parametrize("raw", ["[]", "null", "{", '{"mqtt":[]}'])
def test_invalid_config_is_reported_without_overwriting(tmp_path, raw):
    path = tmp_path / "config.json"
    path.write_text(raw, encoding="utf-8")
    with pytest.raises(ValueError):
        settings.load_config(path)
    assert path.read_text(encoding="utf-8") == raw


@pytest.mark.parametrize("section,key,value", [
    (None, "agent_id", "Bad/Identity"), (None, "agent_id", "_worker"),
    (None, "model", ""), (None, "model", 123), (None, "device", "mps"),
    (None, "host_label", ""), (None, "queue_capacity", True),
    (None, "max_attempts", 11), (None, "result_retry_seconds", 0),
    ("mqtt", "host", "mqtts://broker"), ("mqtt", "host", "broker:8883"),
    ("mqtt", "host", "broker/path"), ("mqtt", "host", "broker name"),
    ("mqtt", "host", "bad!host"), ("mqtt", "host", "-host"),
    ("mqtt", "host", "host..local"),
    ("mqtt", "port", 0), ("mqtt", "port", True), ("mqtt", "port", "8883"),
    ("mqtt", "tls", "true"), ("mqtt", "password_env", "BAD-NAME"),
])
def test_validation_rejects_invalid_known_fields(tmp_path, section, key, value):
    config = configured(tmp_path)
    (config if section is None else config[section])[key] = value
    with pytest.raises(ValueError):
        settings.validate_config(config)


def test_valid_ipv6_local_model_and_advanced_fields_roundtrip_without_password(tmp_path):
    path = tmp_path / "folder" / "settings.json"
    config = configured(tmp_path)
    config["mqtt"].update(host="::1", password="never write this", custom_option={"keep": True})
    config.update(model="C:/Models/Qwen ASR", advanced={"keep": [1, 2]}, password="also secret")
    original = deepcopy(config)
    settings.save_config(path, config)
    value = settings.load_config(path)
    assert "password" not in value and "password" not in value["mqtt"]
    assert value["advanced"] == {"keep": [1, 2]}
    assert value["mqtt"]["custom_option"] == {"keep": True}
    assert config == original
    assert not list(path.parent.glob("*.tmp"))


def test_failed_atomic_replace_preserves_original_and_cleans_temp(tmp_path, monkeypatch):
    path = tmp_path / "settings.json"
    path.write_text('{"original":true}', encoding="utf-8")
    monkeypatch.setattr(settings.os, "replace", Mock(side_effect=OSError("private details")))
    with pytest.raises(ValueError, match="无法保存"):
        settings.save_config(path, configured(tmp_path))
    assert path.read_text(encoding="utf-8") == '{"original":true}'
    assert list(tmp_path.iterdir()) == [path]


def test_credentials_use_mock_store_and_resolved_path_identity(tmp_path, monkeypatch):
    values = {}
    store = SimpleNamespace(
        get_password=lambda service, key: values.get((service, key)),
        set_password=lambda service, key, value: values.__setitem__((service, key), value),
        delete_password=lambda service, key: values.pop((service, key)),
    )
    monkeypatch.setattr(settings, "_keyring", lambda: store)
    path = tmp_path / "sub" / ".." / "config.json"
    assert settings.get_password(path) is None
    settings.save_password(path, "mock-secret")
    assert values == {("orbit-asr-worker", str(path.resolve())): "mock-secret"}
    assert settings.get_password(path.resolve()) == "mock-secret"
    assert settings.get_password(tmp_path / "different.json") is None
    settings.delete_password(path)
    settings.delete_password(path)  # Removing an absent credential is harmless.
    assert values == {}


@pytest.mark.parametrize("function,args", [
    (settings.get_password, ()), (settings.save_password, ("mock-secret",)),
    (settings.delete_password, ()),
])
def test_keyring_failures_are_actionable_and_do_not_expose_details(tmp_path, monkeypatch, function, args):
    monkeypatch.setattr(settings, "_keyring", Mock(side_effect=RuntimeError("mock-secret")))
    with pytest.raises(ValueError) as error:
        function(tmp_path / "config.json", *args)
    assert "凭据" in str(error.value)
    assert "mock-secret" not in str(error.value)
