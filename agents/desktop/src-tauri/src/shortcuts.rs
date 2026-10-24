use crate::{config::Config, wire};
use base64::{engine::general_purpose::STANDARD, Engine};
use serde::{Deserialize, Serialize};
use std::{
    path::{Path, PathBuf},
    process::Command,
};

#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Shortcut {
    pub id: String,
    pub title: String,
    pub kind: String,
    pub target: String,
    #[serde(default)]
    pub icon: String,
}
// The OS chooses the registered handler; no protocol allowlist for saved shortcuts.
pub fn shortcut_url(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 4096
        && value.trim() == value
        && !value.chars().any(char::is_control)
        && url::Url::parse(value).is_ok()
}
pub fn web_url(value: &str) -> bool {
    url::Url::parse(value).is_ok_and(|u| {
        matches!(u.scheme(), "http" | "https")
            && u.host_str().is_some()
            && u.username().is_empty()
            && u.password().is_none()
    }) && value.len() <= 4096
}
pub fn app_path(path: &Path) -> bool {
    if !path.is_absolute() {
        return false;
    }
    #[cfg(target_os = "macos")]
    {
        path.extension().is_some_and(|s| s == "app") && path.is_dir()
    }
    #[cfg(windows)]
    {
        path.extension()
            .is_some_and(|s| matches!(s.to_string_lossy().to_lowercase().as_str(), "exe" | "lnk"))
            && path.is_file()
    }
    #[cfg(not(any(target_os = "macos", windows)))]
    {
        path.is_file()
    }
}
pub fn validate(items: &[Shortcut]) -> Result<(), String> {
    if items.len() > 24 {
        return Err("最多添加 24 个快捷入口".into());
    }
    let mut ids = std::collections::HashSet::new();
    let mut bytes = 0;
    for i in items {
        if uuid::Uuid::parse_str(&i.id).is_err()
            || !ids.insert(&i.id)
            || i.title.trim().is_empty()
            || i.title.len() > 160
        {
            return Err("请填写有效且不重复的快捷入口".into());
        }
        match i.kind.as_str() {
            "url" if shortcut_url(&i.target) => {}
            "app" if app_path(Path::new(&i.target)) => {}
            _ => return Err("请选择本机应用或填写包含协议的完整网址".into()),
        }
        let icon = STANDARD.decode(&i.icon).map_err(|_| "图标格式不正确")?;
        if icon.len() > 32768 || (!icon.is_empty() && !icon.starts_with(b"\x89PNG\r\n\x1a\n")) {
            return Err("图标需为不超过 32 KB 的 PNG".into());
        };
        bytes += icon.len() + i.title.len() + 128;
    }
    if bytes > 240 * 1024 {
        return Err("图标总大小超出限制，请减少入口或缩小图标".into());
    };
    Ok(())
}
pub fn catalog(c: &Config) -> wire::AgentShortcuts {
    wire::AgentShortcuts {
        agent_id: c.agent_id.clone(),
        host_label: c.host_label.clone(),
        revision: c.shortcuts_revision.max(1),
        show_titles: c.shortcuts_show_titles,
        at_top: c.shortcuts_at_top,
        nfcd_metrics_left_padding: Some(c.nfcd_metrics_left_padding),
        items: c
            .shortcuts
            .iter()
            .map(|i| wire::AgentShortcut {
                id: i.id.clone(),
                title: i.title.clone(),
                kind: i.kind.clone(),
                icon_png: STANDARD.decode(&i.icon).unwrap_or_default(),
            })
            .collect(),
    }
}
pub fn import_app(raw: &str) -> Result<Shortcut, String> {
    let path = PathBuf::from(raw)
        .canonicalize()
        .map_err(|_| "找不到所选应用")?;
    if !app_path(&path) {
        return Err("请拖入应用程序（macOS .app，Windows .exe 或 .lnk）".into());
    }
    let title = Path::new(raw)
        .file_stem()
        .unwrap_or_default()
        .to_string_lossy()
        .to_string();
    #[cfg(not(windows))]
    let mut icon = String::new();
    #[cfg(target_os = "macos")]
    {
        let output = Command::new("/usr/bin/plutil")
            .args(["-convert", "json", "-o", "-"])
            .arg(path.join("Contents/Info.plist"))
            .output()
            .map_err(|_| "无法读取应用信息")?;
        if output.status.success() {
            let info: serde_json::Value =
                serde_json::from_slice(&output.stdout).map_err(|_| "应用信息格式不正确")?;
            let resources = path.join("Contents/Resources");
            let name = info["CFBundleIconFile"]
                .as_str()
                .or_else(|| info["CFBundleIconName"].as_str())
                .unwrap_or("");
            let mut candidate = resources.join(name);
            if !candidate.is_file() {
                candidate = resources.join(format!("{name}.icns"))
            }
            if !candidate.is_file() {
                if let Ok(files) = std::fs::read_dir(&resources) {
                    if let Some(p) = files
                        .flatten()
                        .map(|e| e.path())
                        .find(|p| p.extension().is_some_and(|e| e == "icns"))
                    {
                        candidate = p
                    }
                }
            }
            if candidate.is_file() {
                icon = import_icon(candidate.to_str().ok_or("应用路径无效")?).unwrap_or_default()
            }
        }
    }
    #[cfg(target_os = "macos")]
    if icon.is_empty() {
        icon = macos_icon(&path).unwrap_or_default();
    }
    #[cfg(windows)]
    let (title, icon) = {
        let metadata = crate::windows_apps::metadata(&path);
        (metadata.0.unwrap_or(title), metadata.1)
    };
    Ok(Shortcut {
        id: uuid::Uuid::new_v4().to_string(),
        title,
        kind: "app".into(),
        target: path.display().to_string(),
        icon,
    })
}
#[cfg(target_os = "macos")]
fn macos_icon(path: &Path) -> Result<String, String> {
    let bytes = objc2::rc::autoreleasepool(|_| {
        let image = objc2_app_kit::NSWorkspace::sharedWorkspace().iconForFile(
            &objc2_foundation::NSString::from_str(&path.to_string_lossy()),
        );
        image.TIFFRepresentation().map(|data| data.to_vec())
    })
    .ok_or("无法读取应用图标")?;
    let temp = std::env::temp_dir().join(format!("orbit-app-icon-{}.tiff", uuid::Uuid::new_v4()));
    std::fs::write(&temp, bytes).map_err(|_| "无法读取应用图标")?;
    let result = import_icon(temp.to_str().ok_or("图标路径无效")?);
    let _ = std::fs::remove_file(temp);
    result
}
pub fn import_icon(raw: &str) -> Result<String, String> {
    let path = Path::new(raw);
    if !path.is_file() {
        return Err("找不到图标文件".into());
    }
    #[cfg(target_os = "macos")]
    {
        let temp = std::env::temp_dir().join(format!("orbit-icon-{}.png", uuid::Uuid::new_v4()));
        let result = Command::new("/usr/bin/sips")
            .args(["-s", "format", "png", "-Z", "64"])
            .arg(path)
            .arg("--out")
            .arg(&temp)
            .output();
        let bytes = std::fs::read(&temp);
        let _ = std::fs::remove_file(&temp);
        if !result.is_ok_and(|r| r.status.success()) {
            return Err("无法读取图标，请选择图片文件".into());
        }
        encoded_png(bytes.map_err(|_| "图标转换失败")?)
    }
    #[cfg(windows)]
    {
        windows_image(path)
    }
    #[cfg(not(any(target_os = "macos", windows)))]
    {
        encoded_png(std::fs::read(path).map_err(|_| "无法读取图标")?)
    }
}
pub(crate) fn encoded_png(b: Vec<u8>) -> Result<String, String> {
    if b.len() > 32768 || !b.starts_with(b"\x89PNG\r\n\x1a\n") {
        return Err("请使用不超过 32 KB 的 PNG 图标".into());
    };
    Ok(STANDARD.encode(b))
}
#[cfg(windows)]
fn windows_image(path: &Path) -> Result<String, String> {
    use std::os::windows::process::CommandExt;
    // Paths are environment values, never interpolated into PowerShell code.
    let script = r#"Add-Type -AssemblyName System.Drawing; $im=[Drawing.Image]::FromFile($env:ORBIT_ICON_INPUT); $b=New-Object Drawing.Bitmap 64,64; $g=[Drawing.Graphics]::FromImage($b); $g.DrawImage($im,0,0,64,64); $m=New-Object IO.MemoryStream; $b.Save($m,[Drawing.Imaging.ImageFormat]::Png); [Console]::Write([Convert]::ToBase64String($m.ToArray())); $g.Dispose(); $im.Dispose(); $b.Dispose(); $m.Dispose()"#;
    let out = Command::new("powershell.exe")
        .args(["-NoProfile", "-NonInteractive", "-Command", script])
        .env("ORBIT_ICON_INPUT", path)
        .creation_flags(0x08000000)
        .output()
        .map_err(|_| "无法读取应用图标")?;
    if !out.status.success() {
        return Err("无法读取图标".into());
    };
    let b = STANDARD
        .decode(String::from_utf8_lossy(&out.stdout).trim())
        .map_err(|_| "图标格式不正确")?;
    encoded_png(b)
}
pub(crate) async fn open_shortcut(item: Shortcut) -> Result<(), ()> {
    if item.kind == "app" && !app_path(Path::new(&item.target)) {
        return Err(());
    }
    if item.kind == "url" && !shortcut_url(&item.target) {
        return Err(());
    }
    #[cfg(windows)]
    if item.kind == "app" && item.target.to_lowercase().ends_with(".exe") {
        use std::os::windows::process::CommandExt;
        return Command::new(&item.target)
            .creation_flags(0x08000000)
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()
            .map(|_| ())
            .map_err(|_| ());
    }
    let commands = if item.kind == "app" {
        #[cfg(target_os = "macos")]
        {
            let mut c = Command::new("/usr/bin/open");
            c.arg("-a").arg(&item.target);
            vec![c]
        }
        #[cfg(windows)]
        {
            if item.target.to_lowercase().ends_with(".exe") {
                vec![Command::new(&item.target)]
            } else {
                open::commands(&item.target)
            }
        }
        #[cfg(not(any(target_os = "macos", windows)))]
        {
            vec![Command::new(&item.target)]
        }
    } else {
        open::commands(&item.target)
    };
    for command in commands {
        let mut c = tokio::process::Command::from(command);
        c.kill_on_drop(true)
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null());
        #[cfg(windows)]
        c.creation_flags(0x08000000);
        if c.status().await.is_ok_and(|s| s.success()) {
            return Ok(());
        }
    }
    Err(())
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn navigation_position_defaults_to_bottom_and_survives_settings_roundtrip() {
        let mut c: Config = serde_json::from_str(r#"{"agentId":"test"}"#).unwrap();
        assert!(!c.shortcuts_at_top);
        assert!(!catalog(&c).at_top);
        for at_top in [true, false] {
            c.shortcuts_at_top = at_top;
            let saved = serde_json::to_value(&c).unwrap();
            assert_eq!(saved["shortcutsAtTop"], at_top);
            c = serde_json::from_value(saved).unwrap();
            assert_eq!(catalog(&c).at_top, at_top);
        }
    }

    #[test]
    fn nfcd_padding_defaults_and_roundtrips_zero() {
        let mut c: Config = serde_json::from_str(r#"{"agentId":"test"}"#).unwrap();
        assert_eq!(c.nfcd_metrics_left_padding, 16);
        c.mqtt.url = "mqtts://example.test:8883".into();
        for padding in [0, 16, 64] {
            c.nfcd_metrics_left_padding = padding;
            c = serde_json::from_value(serde_json::to_value(&c).unwrap()).unwrap();
            assert_eq!(catalog(&c).nfcd_metrics_left_padding, Some(padding));
            assert!(c.validate(&crate::config::Secrets::default()).is_ok());
        }
        c.nfcd_metrics_left_padding = 65;
        assert!(c.validate(&crate::config::Secrets::default()).is_err());
    }

    #[test]
    fn website_fetch_only_accepts_http_without_credentials() {
        for v in [
            "file:///tmp/x",
            "javascript:alert(1)",
            "https://a:b@example.com",
            "codex://threads/x",
        ] {
            assert!(!web_url(v))
        }
        assert!(web_url("https://example.com/a?q=hello"));
    }
    #[test]
    fn shortcuts_accept_absolute_urls_with_any_scheme() {
        for target in [
            "ftp://192.168.1.1",
            "codex://threads/01a13dc3-58a7-7861-95ed-dee3470de4fd",
            "mailto:hello@example.com",
            "file:///tmp/example.txt",
            "my-app+v2:open?name=test",
            "http://example.com",
            "https://example.com",
        ] {
            let item = Shortcut {
                id: uuid::Uuid::new_v4().to_string(),
                title: "Test".into(),
                kind: "url".into(),
                target: target.into(),
                icon: String::new(),
            };
            assert!(shortcut_url(target), "{target}");
            assert!(validate(&[item]).is_ok(), "{target}");
        }
        for target in [
            "",
            "example.com",
            "/tmp/example",
            "https://",
            "--help",
            "https://example.com\n",
        ] {
            assert!(!shortcut_url(target), "{target:?}");
        }
    }

    #[test]
    fn catalog_does_not_export_targets() {
        let mut c = Config::default();
        c.shortcuts.push(Shortcut {
            id: uuid::Uuid::new_v4().to_string(),
            title: "Test".into(),
            kind: "url".into(),
            target: "https://private.example/path".into(),
            icon: String::new(),
        });
        use prost::Message;
        assert!(!String::from_utf8_lossy(&catalog(&c).encode_to_vec()).contains("private.example"));
    }
}
