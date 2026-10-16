//! Updates use the published GitHub asset and its API-provided SHA-256 digest.
//! No URL, checksum or installation path is accepted from the webview.
use semver::Version;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{sync::Mutex, time::Duration};
use tauri::Manager;
use tokio::{io::AsyncWriteExt, sync::Mutex as AsyncMutex};

const RELEASE_URL: &str = "https://api.github.com/repos/leowzz/orbit/releases/latest";
const MAX_PACKAGE: u64 = 512 * 1024 * 1024;

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Status {
    pub current_version: String,
    pub latest_version: Option<String>,
    pub available: bool,
    pub supported: bool,
    pub phase: String,
    pub downloaded: u64,
    pub total: u64,
    pub error: String,
}

#[derive(Clone, Deserialize)]
struct Asset {
    name: String,
    browser_download_url: String,
    digest: Option<String>,
    size: u64,
}

#[derive(Deserialize)]
struct Release {
    tag_name: String,
    draft: bool,
    prerelease: bool,
    assets: Vec<Asset>,
}

pub struct Updater {
    status: Mutex<Status>,
    selected: Mutex<Option<Asset>>,
    operation: AsyncMutex<()>,
}

impl Updater {
    pub fn new(version: String) -> Self {
        Self {
            status: Mutex::new(Status {
                current_version: version,
                latest_version: None,
                available: false,
                supported: cfg!(target_os = "macos"),
                phase: "idle".into(),
                downloaded: 0,
                total: 0,
                error: String::new(),
            }),
            selected: Mutex::new(None),
            operation: AsyncMutex::new(()),
        }
    }

    pub fn snapshot(&self) -> Status {
        self.status.lock().unwrap().clone()
    }

    fn phase(&self, phase: &str) {
        let mut status = self.status.lock().unwrap();
        status.phase = phase.into();
        status.error.clear();
    }

    fn finish(&self, result: Result<(), String>) -> Result<Status, String> {
        let mut status = self.status.lock().unwrap();
        status.phase = "idle".into();
        if let Err(error) = result {
            status.error = error.clone();
            return Err(error);
        }
        Ok(status.clone())
    }

    pub async fn check(&self) -> Result<Status, String> {
        let _guard = self.operation.try_lock().map_err(|_| "更新操作正在进行")?;
        self.phase("checking");
        let result = self.check_inner().await;
        self.finish(result)
    }

    async fn check_inner(&self) -> Result<(), String> {
        let response = client(Duration::from_secs(30))?
            .get(RELEASE_URL)
            .send()
            .await
            .map_err(|_| "无法连接更新服务，请检查网络后重试")?;
        if response.status().as_u16() == 404 {
            return Err("暂时没有可用的正式版本".into());
        }
        let release: Release = response
            .error_for_status()
            .map_err(|_| "更新服务暂不可用，请稍后重试")?
            .json()
            .await
            .map_err(|_| "更新信息无效，请稍后重试")?;
        // Clear a previous offer even when a newly published release is incomplete.
        *self.selected.lock().unwrap() = None;
        self.status.lock().unwrap().available = false;
        let current =
            Version::parse(&self.snapshot().current_version).map_err(|_| "无法识别当前版本")?;
        let (version, asset) = select_release(release, &current)?;
        let mut status = self.status.lock().unwrap();
        status.latest_version = Some(version.to_string());
        status.available = asset.is_some() && status.supported;
        *self.selected.lock().unwrap() = asset;
        Ok(())
    }

    pub async fn install(&self, app: tauri::AppHandle) -> Result<Status, String> {
        let _guard = self.operation.try_lock().map_err(|_| "更新操作正在进行")?;
        self.phase("downloading");
        let result = self.install_inner(&app).await;
        if let Err(error) = result {
            return self.finish(Err(error));
        }
        self.phase("restarting");
        // The helper waits for this process before replacing the bundle.
        app.exit(0);
        Ok(self.snapshot())
    }

    async fn install_inner(&self, app: &tauri::AppHandle) -> Result<(), String> {
        if !cfg!(target_os = "macos") {
            return Err("此平台暂不支持应用内更新".into());
        }
        let asset = self
            .selected
            .lock()
            .unwrap()
            .clone()
            .ok_or("请先检查更新")?;
        let version = self.snapshot().latest_version.ok_or("请先检查更新")?;
        let cache = app.path().app_cache_dir().map_err(|_| "无法打开更新缓存")?;
        tokio::fs::create_dir_all(&cache)
            .await
            .map_err(|_| "无法创建更新缓存")?;
        let temporary = tempfile::Builder::new()
            .prefix("update-")
            .tempdir_in(&cache)
            .map_err(|_| "无法创建更新缓存")?;
        let package = temporary.path().join("update.dmg");
        let mut response = client(Duration::from_secs(15 * 60))?
            .get(&asset.browser_download_url)
            .send()
            .await
            .map_err(|_| "下载失败，请检查网络后重试")?
            .error_for_status()
            .map_err(|_| "更新包暂不可用，请稍后重试")?;
        let mut file = tokio::fs::File::create(&package)
            .await
            .map_err(|_| "无法保存更新包")?;
        let mut hash = Sha256::new();
        let mut downloaded = 0;
        {
            let mut status = self.status.lock().unwrap();
            status.downloaded = 0;
            status.total = asset.size;
        }
        while let Some(chunk) = response.chunk().await.map_err(|_| "下载中断，请重试")? {
            downloaded += chunk.len() as u64;
            if downloaded > asset.size || downloaded > MAX_PACKAGE {
                return Err("更新包大小不符，请重新检查更新".into());
            }
            hash.update(&chunk);
            file.write_all(&chunk)
                .await
                .map_err(|_| "无法保存更新包，请检查磁盘空间")?;
            self.status.lock().unwrap().downloaded = downloaded;
        }
        file.sync_all().await.map_err(|_| "无法保存更新包")?;
        drop(file);
        verify_download(&asset, downloaded, &hex::encode(hash.finalize()))?;
        self.phase("installing");
        #[cfg(target_os = "macos")]
        {
            tokio::task::spawn_blocking(move || macos::prepare_and_launch(temporary, &version))
                .await
                .map_err(|_| "无法启动更新，请重试")??;
        }
        #[cfg(not(target_os = "macos"))]
        let _ = (temporary, version);
        Ok(())
    }
}

fn client(timeout: Duration) -> Result<reqwest::Client, String> {
    reqwest::Client::builder()
        .https_only(true)
        .user_agent("Orbit-Agent-Updater")
        .connect_timeout(Duration::from_secs(15))
        .timeout(timeout)
        .build()
        .map_err(|_| "无法初始化更新连接".into())
}

fn select_release(release: Release, current: &Version) -> Result<(Version, Option<Asset>), String> {
    let version = Version::parse(
        release
            .tag_name
            .strip_prefix('v')
            .unwrap_or(&release.tag_name),
    )
    .map_err(|_| "更新版本号无效")?;
    if release.draft || release.prerelease || !version.pre.is_empty() {
        return Err("更新服务未返回正式版本".into());
    }
    if version <= *current {
        return Ok((version, None));
    }
    let name = format!("orbit-agent-macos-universal-{}.dmg", release.tag_name);
    let asset = release
        .assets
        .into_iter()
        .find(|asset| asset.name == name)
        .ok_or("此版本尚未提供 Mac 更新包，请稍后重试")?;
    let expected = format!(
        "https://github.com/leowzz/orbit/releases/download/{}/{name}",
        release.tag_name
    );
    let digest = asset
        .digest
        .as_deref()
        .and_then(|value| value.strip_prefix("sha256:"))
        .ok_or("更新包缺少校验信息")?;
    if asset.browser_download_url != expected
        || asset.size == 0
        || asset.size > MAX_PACKAGE
        || digest.len() != 64
        || !digest.bytes().all(|b| b.is_ascii_hexdigit())
    {
        return Err("更新包信息无效，请稍后重试".into());
    }
    Ok((version, Some(asset)))
}

fn verify_download(asset: &Asset, size: u64, hash: &str) -> Result<(), String> {
    if size != asset.size || asset.digest.as_deref() != Some(&format!("sha256:{hash}")) {
        return Err("更新包校验失败，未安装任何文件，请重试".into());
    }
    Ok(())
}

#[cfg(target_os = "macos")]
mod macos {
    use std::{
        path::{Path, PathBuf},
        process::{Command, Stdio},
    };

    const SIGNING_CERTIFICATE: &[u8] = include_bytes!("../../signing/macos-agent.cer");

    fn verify_signature(app: &Path) -> Result<(), String> {
        run(
            "/usr/bin/codesign",
            &[
                "--verify".as_ref(),
                "--deep".as_ref(),
                "--strict".as_ref(),
                app.as_os_str(),
            ],
        )
        .map_err(|_| "更新包签名校验失败，未修改当前应用".to_string())?;
        let directory = tempfile::tempdir().map_err(|_| "无法校验更新证书")?;
        let prefix = directory.path().join("certificate-");
        run(
            "/usr/bin/codesign",
            &[
                "--display".as_ref(),
                format!("--extract-certificates={}", prefix.display()).as_ref(),
                app.as_os_str(),
            ],
        )
        .map_err(|_| "更新包缺少发布证书".to_string())?;
        let leaf = std::fs::read(directory.path().join("certificate-0"))
            .map_err(|_| "更新包缺少发布证书")?;
        if leaf != SIGNING_CERTIFICATE {
            return Err("更新包不是由 Orbit 发布密钥签署，未修改当前应用".into());
        }
        Ok(())
    }

    fn run(program: &str, args: &[&std::ffi::OsStr]) -> Result<(), String> {
        let output = Command::new(program)
            .args(args)
            .output()
            .map_err(|_| "无法准备更新")?;
        if !output.status.success() {
            return Err("无法准备更新，请确认应用安装在可写目录且磁盘空间充足".into());
        }
        Ok(())
    }

    fn plist(app: &Path, field: &str) -> Result<String, String> {
        let output = Command::new("/usr/libexec/PlistBuddy")
            .args(["-c", &format!("Print :{field}")])
            .arg(app.join("Contents/Info.plist"))
            .output()
            .map_err(|_| "无法读取更新包")?;
        if !output.status.success() {
            return Err("更新包不是有效的应用".into());
        }
        String::from_utf8(output.stdout)
            .map(|s| s.trim().to_string())
            .map_err(|_| "更新包信息无效".into())
    }

    fn bundle() -> Result<PathBuf, String> {
        let executable = std::env::current_exe().map_err(|_| "无法定位当前应用")?;
        let app = executable
            .ancestors()
            .find(|p| p.extension().is_some_and(|e| e == "app"))
            .ok_or("请将 Orbit Agent 安装到应用程序目录后再更新")?
            .to_path_buf();
        if app.starts_with("/Volumes") || app.to_string_lossy().contains("/AppTranslocation/") {
            return Err("请将 Orbit Agent 移到应用程序目录后再更新".into());
        }
        Ok(app)
    }

    pub fn prepare_and_launch(temporary: tempfile::TempDir, version: &str) -> Result<(), String> {
        let target = bundle()?;
        let mount = temporary.path().join("mounted");
        std::fs::create_dir(&mount).map_err(|_| "无法准备更新目录")?;
        run(
            "/usr/bin/hdiutil",
            &[
                "attach".as_ref(),
                temporary.path().join("update.dmg").as_os_str(),
                "-readonly".as_ref(),
                "-nobrowse".as_ref(),
                "-mountpoint".as_ref(),
                mount.as_os_str(),
            ],
        )?;
        let stage = target
            .parent()
            .ok_or("应用位置无效")?
            .join(format!(".orbit-update-{}.app", uuid::Uuid::new_v4()));
        let result = (|| {
            let source = mount.join("Orbit Agent.app");
            if plist(&source, "CFBundleIdentifier")? != "dev.orbit.agent"
                || plist(&source, "CFBundleShortVersionString")? != version
            {
                return Err("更新包的应用或版本不符，未修改当前应用".into());
            }
            verify_signature(&source)?;
            run("/usr/bin/ditto", &[source.as_os_str(), stage.as_os_str()])?;
            verify_signature(&stage)?;
            Ok(())
        })();
        // Never leave a mounted image behind, including validation/copy errors.
        let detached =
            run("/usr/bin/hdiutil", &["detach".as_ref(), mount.as_os_str()]).or_else(|_| {
                run(
                    "/usr/bin/hdiutil",
                    &["detach".as_ref(), "-force".as_ref(), mount.as_os_str()],
                )
            });
        if let Err(error) = result.and(detached) {
            let _ = std::fs::remove_dir_all(&stage);
            return Err(error);
        }
        let helper = temporary.path().join("install.sh");
        let launched: Result<(), String> = (|| {
            std::fs::write(&helper, include_str!("update_install.sh"))
                .map_err(|_| "无法创建更新程序")?;
            let log = std::fs::File::create(temporary.path().join("install.log"))
                .map_err(|_| "无法创建更新日志")?;
            Command::new("/bin/bash")
                .arg(&helper)
                .arg(std::process::id().to_string())
                .arg(&target)
                .arg(&stage)
                .arg(temporary.path())
                .stdin(Stdio::null())
                .stdout(Stdio::from(
                    log.try_clone().map_err(|_| "无法创建更新日志")?,
                ))
                .stderr(Stdio::from(log))
                .spawn()
                .map_err(|_| "无法启动更新程序")?;
            Ok(())
        })();
        if launched.is_err() {
            let _ = std::fs::remove_dir_all(stage);
        }
        launched?;
        let _ = temporary.keep();
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn release(version: &str) -> Release {
        serde_json::from_value(serde_json::json!({
            "tag_name": version, "draft": false, "prerelease": false,
            "assets": [{"name": format!("orbit-agent-macos-universal-{version}.dmg"),
                "browser_download_url": format!("https://github.com/leowzz/orbit/releases/download/{version}/orbit-agent-macos-universal-{version}.dmg"),
                "digest": format!("sha256:{}", "a".repeat(64)), "size": 10}]
        })).unwrap()
    }
    #[test]
    fn selects_only_newer_stable_release() {
        let current = Version::parse("0.3.9").unwrap();
        assert!(select_release(release("v0.3.10"), &current)
            .unwrap()
            .1
            .is_some());
        for v in ["v0.3.9", "v0.3.1"] {
            assert!(select_release(release(v), &current).unwrap().1.is_none());
        }
        assert!(select_release(release("v0.4.0-rc.1"), &current).is_err());
        let mut draft = release("v0.4.0");
        draft.draft = true;
        assert!(select_release(draft, &current).is_err());
    }
    #[test]
    fn rejects_untrusted_or_incomplete_assets() {
        let current = Version::parse("0.1.0").unwrap();
        let mut r = release("v0.3.2");
        r.assets[0].browser_download_url = "https://example.com/app.dmg".into();
        assert!(select_release(r, &current).is_err());
        let mut r = release("v0.3.2");
        r.assets[0].digest = None;
        assert!(select_release(r, &current).is_err());
        let mut r = release("v0.3.2");
        r.assets.clear();
        assert!(select_release(r, &current).is_err());
    }
    #[test]
    fn rejects_corrupt_and_truncated_downloads() {
        let asset = release("v0.3.2").assets.remove(0);
        assert!(verify_download(&asset, 10, &"a".repeat(64)).is_ok());
        assert!(verify_download(&asset, 9, &"a".repeat(64)).is_err());
        assert!(verify_download(&asset, 10, &"b".repeat(64)).is_err());
    }

    #[cfg(unix)]
    #[test]
    fn helper_replaces_bundle_and_rolls_back_failed_launch() {
        for fail_launch in [false, true] {
            let root = tempfile::tempdir().unwrap();
            let target = root.path().join("Orbit Agent.app");
            let stage = root.path().join(".orbit-stage.app");
            let work = root.path().join("work");
            for dir in [&target, &stage, &work] {
                std::fs::create_dir(dir).unwrap();
            }
            std::fs::write(target.join("version"), "old").unwrap();
            std::fs::write(stage.join("version"), "new").unwrap();
            // Only replace the launcher in the test copy. Moves and rollback
            // run against real temporary bundles, including paths with spaces.
            let launcher = root.path().join("open-test.sh");
            std::fs::write(&launcher, "if [ \"$FAIL_LAUNCH\" = true ] && [ \"$(cat \"$1/version\")\" = new ]; then exit 1; fi\nexit 0\n").unwrap();
            let helper = work.join("install.sh");
            std::fs::write(
                &helper,
                include_str!("update_install.sh")
                    .replace("/usr/bin/open", "bash \"$TEST_LAUNCHER\""),
            )
            .unwrap();
            let result = std::process::Command::new("bash")
                .arg(&helper)
                .arg("99999999")
                .arg(&target)
                .arg(&stage)
                .arg(&work)
                .env("TEST_LAUNCHER", &launcher)
                .env("FAIL_LAUNCH", fail_launch.to_string())
                .status()
                .unwrap();
            assert_eq!(result.success(), !fail_launch);
            assert_eq!(
                std::fs::read_to_string(target.join("version")).unwrap(),
                if fail_launch { "old" } else { "new" }
            );
            assert!(!stage.exists());
            assert!(!root.path().join(".orbit-stage.previous.app").exists());
        }
    }
}
