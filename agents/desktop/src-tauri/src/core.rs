use crate::config::{Config, Mqtt};
use serde::Deserialize;

#[derive(Clone)]
pub struct ProvisionedMqtt {
    pub mqtt: Mqtt,
    pub password: String,
    pub node_id: String,
}

#[derive(Debug, Deserialize)]
struct Response {
    #[serde(default)]
    enabled: bool,
    mqtt: Option<Credentials>,
    code: Option<String>,
    message: Option<String>,
}

#[derive(Debug, Deserialize)]
struct Credentials {
    uri: String,
    #[serde(rename = "nodeId")]
    node_id: String,
    username: String,
    password: String,
}

/// Fetches a broker credential from Core's authenticated App API. The token is
/// kept in the OS credential store by `config::Store` and sent only to the
/// validated Core URL.
pub async fn provision_mqtt(config: &Config, token: &str) -> Result<ProvisionedMqtt, String> {
    let base = config.core.url.trim().trim_end_matches('/');
    if base.is_empty() || token.trim().is_empty() {
        return Err("请填写 Core 地址和设备令牌".into());
    }
    let endpoint = format!("{base}/api/v1/mqtt");
    let response = reqwest::Client::new()
        .post(endpoint)
        .bearer_auth(token.trim())
        .json(&serde_json::json!({}))
        .send()
        .await
        .map_err(|_| "无法连接 Core，请检查地址和网络".to_string())?;
    let status = response.status();
    let value: Response = response
        .json()
        .await
        .map_err(|_| "Core 返回的 MQTT 配置格式不正确".to_string())?;
    if !status.is_success() {
        return Err(match (value.code.as_deref(), value.message.as_deref()) {
            (Some("unauthenticated"), _) => "Core 设备令牌无效或已撤销".into(),
            (Some("forbidden"), _) => "该 Core 设备不允许使用 MQTT Agent".into(),
            (_, Some(message)) if !message.is_empty() => message.to_string(),
            _ => format!("Core 未能下发 MQTT 配置（HTTP {}）", status.as_u16()),
        });
    }
    if !value.enabled {
        return Err("Core 未启用 MQTT 凭据下发，请检查 mqtt_admin 配置".into());
    }
    let credentials = value.mqtt.ok_or("Core 未返回 MQTT 配置，请检查设备登记")?;
    let uri = url::Url::parse(&credentials.uri).map_err(|_| "Core 返回的 MQTT 地址无效")?;
    if !["mqtt", "mqtts", "tls"].contains(&uri.scheme())
        || uri.host_str().is_none()
        || !uri.username().is_empty()
        || uri.password().is_some()
        || uri.query().is_some()
        || uri.fragment().is_some()
        || !["", "/"].contains(&uri.path())
    {
        return Err("Core 返回的 MQTT 地址无效".into());
    }
    if credentials.node_id.is_empty()
        || credentials.username.is_empty()
        || credentials.password.is_empty()
    {
        return Err("Core 返回的 MQTT 凭据不完整".into());
    }
    Ok(ProvisionedMqtt {
        mqtt: Mqtt {
            url: credentials.uri,
            username: credentials.username,
            ca_file: String::new(),
            cert_file: String::new(),
            key_file: String::new(),
        },
        password: credentials.password,
        node_id: credentials.node_id,
    })
}
