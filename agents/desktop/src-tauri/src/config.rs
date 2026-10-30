use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{
    fs,
    path::{Path, PathBuf},
};

#[derive(Clone, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct Config {
    pub shortcuts: Vec<crate::shortcuts::Shortcut>,
    pub shortcuts_revision: u64,
    pub shortcuts_show_titles: bool,
    pub shortcuts_at_top: bool,
    pub nfcd_metrics_left_padding: u32,
    pub agent_id: String,
    pub host_label: String,
    pub core: Core,
    pub mqtt: Mqtt,
    pub codex: Codex,
    pub usage: Usage,
    pub inference: Inference,
    pub allow_open: bool,
    pub ntp_server: String,
    pub ntp_interval: u64,
    pub ntp_timeout: u64,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct Core {
    pub url: String,
}
impl Default for Core {
    fn default() -> Self {
        Self { url: String::new() }
    }
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
#[derive(Default)]
pub struct Mqtt {
    pub url: String,
    pub username: String,
    pub ca_file: String,
    pub cert_file: String,
    pub key_file: String,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct Codex {
    pub enabled: bool,
    pub home: String,
    pub binary: String,
    pub rate_limits: bool,
    pub interval: u64,
    pub ttl: u64,
    pub limit: usize,
    pub include_archived: bool,
    pub include_title: bool,
    pub include_project: bool,
    pub ignore_cwd: Vec<String>,
    pub ignore_source: Vec<String>,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct Usage {
    pub enabled: bool,
    pub base_url: String,
    pub email: String,
    pub timezone: String,
    pub interval: u64,
    pub ttl: u64,
    pub timeout: u64,
    pub login_endpoint: String,
    pub refresh_endpoint: String,
    pub usage_endpoint: String,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct Inference {
    pub enabled: bool,
    pub model_id: String,
    pub agent_id: String,
    pub username: String,
    pub model_path: String,
    pub runtime_path: String,
    pub device: String,
    pub queue_capacity: u32,
}
impl Default for Inference {
    fn default() -> Self {
        Self {
            enabled: false,
            model_id: crate::inference::assets::MODEL_ID.into(),
            agent_id: "sdt-asr".into(),
            username: String::new(),
            model_path: crate::inference::assets::managed_model_path()
                .to_string_lossy()
                .into_owned(),
            runtime_path: crate::inference::assets::managed_runtime_path()
                .to_string_lossy()
                .into_owned(),
            device: "auto".into(),
            queue_capacity: 1000,
        }
    }
}
impl Default for Codex {
    fn default() -> Self {
        Self {
            enabled: true,
            home: String::new(),
            binary: detect_binary(),
            rate_limits: true,
            interval: 5,
            ttl: 15,
            limit: 20,
            include_archived: false,
            include_title: false,
            include_project: false,
            ignore_cwd: vec![],
            ignore_source: vec![],
        }
    }
}
impl Default for Usage {
    fn default() -> Self {
        Self {
            enabled: false,
            base_url: String::new(),
            email: String::new(),
            timezone: "Asia/Shanghai".into(),
            interval: 30,
            ttl: 120,
            timeout: 10,
            login_endpoint: String::new(),
            refresh_endpoint: String::new(),
            usage_endpoint: String::new(),
        }
    }
}
impl Default for Config {
    fn default() -> Self {
        Self {
            shortcuts: vec![],
            shortcuts_revision: 1,
            shortcuts_show_titles: false,
            shortcuts_at_top: false,
            nfcd_metrics_left_padding: 16,
            agent_id: default_id(),
            host_label: std::env::var("COMPUTERNAME")
                .or_else(|_| std::env::var("HOSTNAME"))
                .unwrap_or_else(|_| "我的电脑".into()),
            core: Core::default(),
            mqtt: Mqtt::default(),
            codex: Codex::default(),
            usage: Usage::default(),
            inference: Inference::default(),
            allow_open: false,
            ntp_server: "ntp.aliyun.com".into(),
            ntp_interval: 600,
            ntp_timeout: 2,
        }
    }
}
#[derive(Clone, Default, Serialize, Deserialize)]
pub struct Secrets {
    pub mqtt: String,
    pub usage: String,
    #[serde(default)]
    pub inference: String,
    #[serde(default)]
    pub core: String,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Settings {
    pub config: Config,
    pub core_token: Option<String>,
    pub mqtt_password: Option<String>,
    pub usage_password: Option<String>,
    pub inference_password: Option<String>,
    pub has_mqtt_password: bool,
    pub has_usage_password: bool,
    pub has_inference_password: bool,
    pub has_core_token: bool,
}
impl Settings {
    pub fn masked(config: Config, secrets: &Secrets) -> Self {
        Self {
            config,
            core_token: None,
            mqtt_password: None,
            usage_password: None,
            inference_password: None,
            has_mqtt_password: !secrets.mqtt.is_empty(),
            has_usage_password: !secrets.usage.is_empty(),
            has_inference_password: !secrets.inference.is_empty(),
            has_core_token: !secrets.core.is_empty(),
        }
    }
    pub fn secrets(&self, previous: &Secrets) -> Secrets {
        Secrets {
            mqtt: self
                .mqtt_password
                .clone()
                .unwrap_or_else(|| previous.mqtt.clone()),
            usage: self
                .usage_password
                .clone()
                .unwrap_or_else(|| previous.usage.clone()),
            inference: self
                .inference_password
                .clone()
                .unwrap_or_else(|| previous.inference.clone()),
            core: self
                .core_token
                .clone()
                .unwrap_or_else(|| previous.core.clone()),
        }
    }
}
impl Config {
    pub fn validate(&self, secrets: &Secrets) -> Result<(), String> {
        crate::shortcuts::validate(&self.shortcuts)?;
        if self.nfcd_metrics_left_padding > 64 {
            return Err("NFCD 指标左侧留白需在 0–64 之间".into());
        }
        if self.agent_id.is_empty()
            || self.agent_id.len() > 128
            || !self
                .agent_id
                .chars()
                .all(|c| c.is_ascii_alphanumeric() || "-_.".contains(c))
        {
            return Err("设备标识只能包含字母、数字、横线、下划线和点".into());
        }
        if self.host_label.trim().is_empty() || self.host_label.len() > 192 {
            return Err("请填写有效的本机名称".into());
        }
        if self.mqtt.url.trim().is_empty() {
            if self.core.url.trim().is_empty() {
                return Err("请填写 MQTT 服务地址，或配置 Core 地址和设备令牌".into());
            }
            if secrets.core.trim().is_empty() {
                return Err("请填写 Core 设备令牌，以自动获取 MQTT 配置".into());
            }
        } else {
            let url = url::Url::parse(&self.mqtt.url).map_err(|_| "请填写有效的 MQTT 服务地址")?;
            if !["mqtts", "tls", "mqtt"].contains(&url.scheme())
                || url.host_str().is_none()
                || !url.username().is_empty()
                || url.password().is_some()
                || url.query().is_some()
                || url.fragment().is_some()
                || !["", "/"].contains(&url.path())
            {
                return Err("服务地址需使用 mqtts:// 或 mqtt://，凭据请填写在独立输入框中".into());
            }
        }
        if !self.core.url.trim().is_empty() {
            let url = url::Url::parse(&self.core.url).map_err(|_| "Core 地址无效")?;
            let local_http = url.scheme() == "http"
                && matches!(url.host_str(), Some("localhost" | "127.0.0.1" | "::1"));
            if url.scheme() != "https" && !local_http
                || url.host_str().is_none()
                || !url.username().is_empty()
                || url.password().is_some()
                || url.query().is_some()
                || url.fragment().is_some()
            {
                return Err("Core 地址需使用 HTTPS，凭据请填写在独立输入框中".into());
            }
        }
        if self.mqtt.cert_file.is_empty() != self.mqtt.key_file.is_empty() {
            return Err("客户端证书和私钥需要同时填写".into());
        }
        if !self.codex.enabled && !self.usage.enabled && !self.inference.enabled {
            return Err("请至少启用一种数据来源或本地推理".into());
        }
        if self.allow_open && !self.codex.enabled {
            return Err("允许打开会话前需启用 Codex 来源".into());
        }
        if self.codex.enabled
            && (self.codex.interval == 0
                || self.codex.ttl < self.codex.interval
                || self.codex.ttl > 300
                || !(1..=20).contains(&self.codex.limit))
        {
            return Err(
                "Codex 采集间隔需大于 0，保留时间应介于采集间隔和 300 秒之间，会话数量为 1–20"
                    .into(),
            );
        }
        if self.usage.enabled {
            if self.usage.email.trim().is_empty() || secrets.usage.is_empty() {
                return Err("请填写 Sub2API 邮箱和密码".into());
            }
            for endpoint in self.usage.endpoints() {
                let u = url::Url::parse(&endpoint).map_err(|_| "Sub2API 地址无效")?;
                if u.scheme() != "https"
                    || u.host_str().is_none()
                    || !u.username().is_empty()
                    || u.password().is_some()
                    || u.query().is_some()
                    || u.fragment().is_some()
                {
                    return Err("Sub2API 地址必须是 HTTPS".into());
                }
            }
            if self.usage.timezone.parse::<chrono_tz::Tz>().is_err()
                || self.usage.interval == 0
                || self.usage.ttl < self.usage.interval
                || self.usage.ttl > 300
                || !(1..=60).contains(&self.usage.timeout)
            {
                return Err("请检查 Sub2API 时区、采集间隔和超时设置".into());
            }
        }
        if self.inference.enabled {
            if !audio_identity(&self.inference.agent_id) {
                return Err("本地推理 Worker 标识只能包含小写字母、数字、横线和下划线".into());
            }
            if self.inference.queue_capacity == 0 || self.inference.queue_capacity > 1000 {
                return Err("本地推理队列容量需在 1–1000 之间".into());
            }
            if !matches!(self.inference.device.as_str(), "auto" | "cpu" | "gpu") {
                return Err("本地推理设备只能选择自动、CPU 或 GPU".into());
            }
        }
        if self.ntp_server.trim().is_empty()
            || self.ntp_interval == 0
            || !(1..=30).contains(&self.ntp_timeout)
        {
            return Err("请填写时间服务器".into());
        }
        Ok(())
    }
}
fn audio_identity(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 64
        && value.as_bytes()[0].is_ascii_alphanumeric()
        && value
            .bytes()
            .all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == b'_' || c == b'-')
}
impl Usage {
    pub fn endpoints(&self) -> [String; 3] {
        [
            (&self.login_endpoint, "/api/v1/auth/login"),
            (&self.refresh_endpoint, "/api/v1/auth/refresh"),
            (&self.usage_endpoint, "/api/v1/usage/dashboard/stats"),
        ]
        .map(|(custom, path)| {
            if custom.is_empty() {
                format!("{}{path}", self.base_url.trim_end_matches('/'))
            } else {
                custom.clone()
            }
        })
    }
}

pub fn codex_home(configured: &str) -> PathBuf {
    let value = if configured.trim().is_empty() {
        std::env::var("CODEX_HOME").unwrap_or_else(|_| "~/.codex".into())
    } else {
        configured.into()
    };
    if value == "~" {
        dirs::home_dir().unwrap_or_default()
    } else if let Some(rest) = value.strip_prefix("~/") {
        dirs::home_dir().unwrap_or_default().join(rest)
    } else {
        PathBuf::from(value)
    }
}
pub fn detect_binary() -> String {
    let mut candidates = vec![
        PathBuf::from("/Applications/Codex.app/Contents/Resources/codex"),
        PathBuf::from("/opt/homebrew/bin/codex"),
        PathBuf::from("/usr/local/bin/codex"),
    ];
    if let Ok(path) = std::env::var("PATH") {
        for folder in std::env::split_paths(&path) {
            candidates.push(folder.join(if cfg!(windows) { "codex.exe" } else { "codex" }));
        }
    }
    candidates
        .into_iter()
        .find(|p| p.is_file())
        .map(|p| p.display().to_string())
        .unwrap_or_else(|| "codex".into())
}
pub fn derive_id(uuid: &str) -> String {
    format!(
        "agt_{}",
        &hex::encode(Sha256::digest(
            format!("orbit.agent.v1\0{}", uuid.trim().to_lowercase()).as_bytes()
        ))[..32]
    )
}
fn default_id() -> String {
    #[cfg(target_os = "macos")]
    if let Ok(out) = std::process::Command::new("/usr/sbin/ioreg")
        .args(["-rd1", "-c", "IOPlatformExpertDevice"])
        .output()
    {
        if let Some(line) = String::from_utf8_lossy(&out.stdout)
            .lines()
            .find(|l| l.contains("\"IOPlatformUUID\""))
        {
            if let Some(value) = line.split('=').nth(1) {
                return derive_id(value.trim().trim_matches('"'));
            }
        }
    }
    format!("agt_{}", uuid::Uuid::new_v4().simple())
}

// Import is read-only. Paths in the original YAML remain relative to that file.
pub fn import_yaml(path: &Path) -> Result<Settings, String> {
    let bytes = fs::read(path).map_err(|_| "无法读取配置文件")?;
    if bytes.len() > 1024 * 1024 {
        return Err("配置文件过大".into());
    }
    let v: serde_yaml::Value = serde_yaml::from_slice(&bytes).map_err(|_| "配置文件格式不正确")?;
    let text = |keys: &[&str]| -> String {
        let mut p = &v;
        for k in keys {
            p = &p[*k];
        }
        p.as_str().unwrap_or("").to_string()
    };
    let boolean = |keys: &[&str], fallback: bool| -> bool {
        let mut p = &v;
        for k in keys {
            p = &p[*k];
        }
        p.as_bool().unwrap_or(fallback)
    };
    let duration = |keys: &[&str], fallback: u64| -> Result<u64, String> {
        let s = text(keys);
        if s.is_empty() {
            Ok(fallback)
        } else {
            humantime::parse_duration(&s)
                .map(|d| d.as_secs())
                .map_err(|_| "采集时间格式不正确".into())
        }
    };
    let base = path.parent().unwrap_or(Path::new("."));
    let resolve = |s: String| -> String {
        if s.is_empty() {
            s
        } else if s == "~" || s.starts_with("~/") {
            codex_home(&s).display().to_string()
        } else {
            let p = PathBuf::from(&s);
            if p.is_absolute() {
                s
            } else {
                base.join(p).display().to_string()
            }
        }
    };
    let secret = |keys: &[&str]| -> Result<String, String> {
        let p = resolve(text(keys));
        if p.is_empty() {
            Ok(String::new())
        } else {
            fs::read_to_string(p)
                .map(|s| s.trim_end_matches(['\r', '\n']).to_string())
                .map_err(|_| "无法读取配置引用的凭据文件".into())
        }
    };
    let mut c = Config::default();
    let id = text(&["agent", "id"]);
    if !id.is_empty() {
        c.agent_id = id;
    }
    c.host_label = text(&["agent", "host_label"]);
    c.core.url = text(&["core", "url"]);
    c.mqtt.url = text(&["mqtt", "url"]);
    c.mqtt.username = secret(&["mqtt", "credentials", "username_file"])?;
    c.mqtt.ca_file = resolve(text(&["mqtt", "tls", "ca_file"]));
    c.mqtt.cert_file = resolve(text(&["mqtt", "tls", "cert_file"]));
    c.mqtt.key_file = resolve(text(&["mqtt", "tls", "key_file"]));
    c.codex.enabled = boolean(&["sources", "codex", "enabled"], false);
    c.codex.home = resolve(text(&["sources", "codex", "codex_home"]));
    let binary = text(&["sources", "codex", "binary"]);
    if !binary.is_empty() {
        c.codex.binary = binary;
    }
    c.codex.rate_limits = boolean(&["sources", "codex", "rate_limits"], false);
    c.codex.include_archived = boolean(&["sources", "codex", "include_archived"], false);
    c.codex.include_title = boolean(
        &["sources", "codex", "privacy", "include_display_name"],
        false,
    );
    c.codex.include_project = boolean(
        &["sources", "codex", "privacy", "include_project_name"],
        false,
    );
    c.codex.interval = duration(&["sources", "codex", "poll_interval"], 5)?;
    c.codex.ttl = duration(&["sources", "codex", "observation_ttl"], 15)?;
    c.codex.limit = v["sources"]["codex"]["session_limit"]
        .as_u64()
        .filter(|n| *n > 0)
        .unwrap_or(20) as usize;
    for (key, dest) in [
        ("cwd", &mut c.codex.ignore_cwd),
        ("source", &mut c.codex.ignore_source),
    ] {
        if let Some(values) = v["sources"]["codex"]["ignore"][key].as_sequence() {
            *dest = values
                .iter()
                .filter_map(|x| x.as_str().map(str::to_string))
                .collect();
        }
    }
    c.usage.enabled = boolean(&["sources", "sub2api", "enabled"], false);
    c.usage.base_url = text(&["sources", "sub2api", "base_url"]);
    c.usage.email = secret(&["sources", "sub2api", "credentials", "email_file"])?;
    let zone = text(&["sources", "sub2api", "timezone"]);
    if !zone.is_empty() {
        c.usage.timezone = zone;
    }
    c.usage.interval = duration(&["sources", "sub2api", "poll_interval"], 30)?;
    c.usage.ttl = duration(&["sources", "sub2api", "observation_ttl"], 120)?;
    c.usage.timeout = duration(&["sources", "sub2api", "timeout"], 10)?;
    c.usage.login_endpoint = text(&["sources", "sub2api", "login_endpoint"]);
    c.usage.refresh_endpoint = text(&["sources", "sub2api", "refresh_endpoint"]);
    c.usage.usage_endpoint = text(&["sources", "sub2api", "usage_endpoint"]);
    c.inference.enabled = boolean(&["inference", "enabled"], false);
    c.inference.model_id = text(&["inference", "model_id"]);
    if c.inference.model_id.is_empty() {
        c.inference.model_id = crate::inference::assets::MODEL_ID.into();
    }
    c.inference.agent_id = text(&["inference", "agent_id"]);
    if c.inference.agent_id.is_empty() {
        c.inference.agent_id = Inference::default().agent_id;
    }
    c.inference.username = text(&["inference", "username"]);
    c.inference.model_path = resolve(text(&["inference", "model_path"]));
    c.inference.runtime_path = resolve(text(&["inference", "runtime_path"]));
    let device = text(&["inference", "device"]);
    if !device.is_empty() {
        c.inference.device = device;
    }
    c.inference.queue_capacity = v["inference"]["queue_capacity"].as_u64().unwrap_or(1000) as u32;
    c.allow_open = boolean(&["capabilities", "open_codex_session", "enabled"], false);
    let ntp = text(&["ntp", "server"]);
    if !ntp.is_empty() {
        c.ntp_server = ntp;
    }
    let secrets = Secrets {
        core: secret(&["core", "credentials", "token_file"])?,
        mqtt: secret(&["mqtt", "credentials", "password_file"])?,
        usage: secret(&["sources", "sub2api", "credentials", "password_file"])?,
        inference: secret(&["inference", "credentials", "password_file"])?,
    };
    c.ntp_interval = duration(&["ntp", "sync_interval"], 600)?;
    c.ntp_timeout = duration(&["ntp", "timeout"], 2)?;
    let currency = text(&["sources", "sub2api", "currency_code"]);
    if c.usage.enabled && !currency.is_empty() && currency != "USD" {
        return Err("当前协议仅支持 USD 用量".into());
    }
    if let Some(enabled) = v["mqtt"]["tls"]["enabled"].as_bool() {
        let secure = c.mqtt.url.starts_with("mqtts://") || c.mqtt.url.starts_with("tls://");
        if secure != enabled {
            return Err("MQTT 地址与 TLS 开关不一致".into());
        }
    }
    c.validate(&secrets)?;
    Ok(Settings {
        config: c,
        core_token: Some(secrets.core),
        mqtt_password: Some(secrets.mqtt),
        usage_password: Some(secrets.usage),
        inference_password: Some(secrets.inference),
        has_mqtt_password: false,
        has_usage_password: false,
        has_inference_password: false,
        has_core_token: false,
    })
}

#[derive(Serialize, Deserialize)]
struct Document {
    config: Config,
    secret_id: String,
}
pub struct Store {
    pub path: PathBuf,
}
impl Store {
    pub fn load(&self) -> Result<Option<(Config, Secrets)>, String> {
        if !self.path.exists() {
            return Ok(None);
        }
        let doc: Document =
            serde_json::from_slice(&fs::read(&self.path).map_err(|_| "无法读取设置")?)
                .map_err(|_| "设置文件损坏，请导入配置恢复")?;
        let secret = keyring::Entry::new("dev.orbit.agent", &doc.secret_id)
            .map_err(|_| "无法访问系统凭据存储")?
            .get_password()
            .map_err(|_| "无法读取系统凭据，请解锁凭据存储后重试")?;
        let secrets = serde_json::from_str(&secret).map_err(|_| "凭据格式不正确")?;
        Ok(Some((doc.config, secrets)))
    }
    pub fn save(&self, c: &Config, s: &Secrets) -> Result<(), String> {
        c.validate(s)?;
        let previous = fs::read(&self.path)
            .ok()
            .and_then(|b| serde_json::from_slice::<Document>(&b).ok());
        let id = uuid::Uuid::new_v4().to_string();
        let entry =
            keyring::Entry::new("dev.orbit.agent", &id).map_err(|_| "无法访问系统凭据存储")?;
        entry
            .set_password(&serde_json::to_string(s).map_err(|_| "无法保存凭据")?)
            .map_err(|_| "无法保存到系统凭据存储")?;
        let doc = Document {
            config: c.clone(),
            secret_id: id,
        };
        let result = (|| -> Result<(), String> {
            fs::create_dir_all(self.path.parent().ok_or("配置路径无效")?)
                .map_err(|_| "无法创建设置目录")?;
            let temp = self.path.with_extension("tmp");
            use std::io::Write;
            let mut opts = fs::OpenOptions::new();
            opts.write(true).create(true).truncate(true);
            #[cfg(unix)]
            {
                use std::os::unix::fs::OpenOptionsExt;
                opts.mode(0o600);
            }
            let mut file = opts.open(&temp).map_err(|_| "无法保存设置")?;
            file.write_all(&serde_json::to_vec_pretty(&doc).map_err(|_| "无法序列化设置")?)
                .map_err(|_| "无法保存设置")?;
            file.sync_all().map_err(|_| "无法保存设置")?;
            // Windows rename cannot replace an existing file; use its replacement API.
            replace_file(&temp, &self.path).map_err(|_| "无法替换设置文件")?;
            Ok(())
        })();
        if result.is_err() {
            let _ = entry.delete_credential();
        } else if let Some(old) = previous {
            if let Ok(e) = keyring::Entry::new("dev.orbit.agent", &old.secret_id) {
                let _ = e.delete_credential();
            }
        }
        result
    }
}
#[cfg(not(windows))]
pub(crate) fn replace_file(from: &Path, to: &Path) -> std::io::Result<()> {
    fs::rename(from, to)
}
#[cfg(windows)]
pub(crate) fn replace_file(from: &Path, to: &Path) -> std::io::Result<()> {
    use std::os::windows::ffi::OsStrExt;
    #[link(name = "kernel32")]
    extern "system" {
        fn MoveFileExW(a: *const u16, b: *const u16, flags: u32) -> i32;
    }
    let a: Vec<u16> = from.as_os_str().encode_wide().chain(Some(0)).collect();
    let b: Vec<u16> = to.as_os_str().encode_wide().chain(Some(0)).collect();
    if unsafe { MoveFileExW(a.as_ptr(), b.as_ptr(), 1 | 8) } == 0 {
        Err(std::io::Error::last_os_error())
    } else {
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn legacy_secrets_preserve_credentials_without_inference() {
        let secrets: Secrets =
            serde_json::from_str(r#"{"mqtt":"mqtt-secret","usage":"usage-secret"}"#).unwrap();
        assert_eq!(secrets.mqtt, "mqtt-secret");
        assert_eq!(secrets.usage, "usage-secret");
        assert!(secrets.inference.is_empty());
    }
    #[test]
    fn current_secrets_preserve_inference_and_reject_invalid_fields() {
        let secrets: Secrets = serde_json::from_str(
            r#"{"mqtt":"mqtt-secret","usage":"usage-secret","inference":"asr-secret"}"#,
        )
        .unwrap();
        assert_eq!(secrets.inference, "asr-secret");
        for invalid in [
            r#"{"mqtt":"mqtt-secret","usage":"usage-secret","inference":null}"#,
            r#"{"mqtt":"mqtt-secret","usage":"usage-secret","inference":123}"#,
            r#"{"usage":"usage-secret"}"#,
            r#"{"mqtt":"mqtt-secret"}"#,
        ] {
            assert!(serde_json::from_str::<Secrets>(invalid).is_err());
        }
    }
    #[test]
    fn identity_matches_go() {
        assert_eq!(derive_id(" ABC "), derive_id("abc"));
        assert_eq!(derive_id("abc").len(), 36);
    }
    #[test]
    fn no_credentials_in_view() {
        let c = Config::default();
        let s = Secrets {
            mqtt: "secret".into(),
            usage: "other".into(),
            inference: "asr-secret".into(),
            core: "core-token".into(),
        };
        let text = serde_json::to_string(&Settings::masked(c, &s)).unwrap();
        assert!(!text.contains("secret"));
        assert!(!text.contains("other"));
        assert!(!text.contains("asr-secret"));
    }
    #[test]
    fn inference_can_be_the_only_enabled_capability() {
        let mut c = Config::default();
        c.mqtt.url = "mqtts://example.test:8883".into();
        c.codex.enabled = false;
        c.usage.enabled = false;
        c.inference.enabled = true;
        assert!(c.validate(&Secrets::default()).is_ok());
        c.inference.device = "cuda".into();
        assert!(c.validate(&Secrets::default()).is_err());
    }
    #[test]
    fn core_provisioning_can_supply_mqtt_on_first_start() {
        let mut c = Config::default();
        c.mqtt.url.clear();
        c.core.url = "https://core.example.test".into();
        let mut secrets = Secrets::default();
        secrets.core = "device-token".into();
        assert!(c.validate(&secrets).is_ok());
        secrets.core.clear();
        assert!(c.validate(&secrets).is_err());
    }
    #[test]
    fn import_resolves_secrets_and_preserves_identity() {
        let dir = tempfile::tempdir().unwrap();
        fs::write(dir.path().join("pw"), "test-secret\n").unwrap();
        fs::write(dir.path().join("a.yaml"),"agent:\n  id: test-agent\n  host_label: Test\nmqtt:\n  url: mqtts://example.test:8883\n  credentials:\n    password_file: pw\nsources:\n  codex:\n    enabled: true\n    session_limit: 3\n    poll_interval: 5s\ncapabilities:\n  open_codex_session:\n    enabled: true\n").unwrap();
        let s = import_yaml(&dir.path().join("a.yaml")).unwrap();
        assert_eq!(s.config.agent_id, "test-agent");
        assert_eq!(s.mqtt_password.as_deref(), Some("test-secret"));
        assert_eq!(s.config.codex.limit, 3);
        assert!(s.config.allow_open);
    }
    #[test]
    fn import_expands_home_before_resolving_relative_paths() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("agent.yaml");
        for (configured, expected) in [
            ("~/.codex", dirs::home_dir().unwrap().join(".codex")),
            ("codex-home", dir.path().join("codex-home")),
            ("", PathBuf::new()),
        ] {
            fs::write(
                &path,
                format!("agent:\n  host_label: Test\nmqtt:\n  url: mqtts://example.test:8883\nsources:\n  codex:\n    enabled: true\n    codex_home: '{configured}'\n"),
            )
            .unwrap();
            let imported = import_yaml(&path).unwrap();
            assert_eq!(PathBuf::from(imported.config.codex.home), expected);
        }
    }
}
