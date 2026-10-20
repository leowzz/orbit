pub mod clock;
pub mod codex;
pub mod commands;
pub mod config;
pub mod recent_apps;
pub mod runtime;
pub mod shortcuts;
pub mod update;
pub mod usage;
pub mod website_icon;
pub mod wire {
    include!(concat!(env!("OUT_DIR"), "/orbit.v1.rs"));
}
use config::{Config, Secrets, Settings, Store};
use std::sync::{Arc, Mutex};
use tauri::{
    menu::{Menu, MenuItem},
    tray::TrayIconBuilder,
    Emitter, Manager,
};
use tauri_plugin_autostart::ManagerExt;
use tokio::sync::Mutex as AsyncMutex;
use tokio_util::sync::CancellationToken;
struct Active {
    cancel: CancellationToken,
    task: tauri::async_runtime::JoinHandle<()>,
}
struct Inner {
    config: Config,
    secrets: Secrets,
    active: Option<Active>,
    load_failed: bool,
}
struct Agent {
    inner: AsyncMutex<Inner>,
    store: Store,
    status: runtime::SharedStatus,
}
impl Agent {
    async fn stop(inner: &mut Inner) {
        if let Some(active) = inner.active.take() {
            active.cancel.cancel();
            let _ = active.task.await;
        }
    }
    fn launch(&self, inner: &mut Inner) {
        let cancel = CancellationToken::new();
        let task = tauri::async_runtime::spawn(runtime::run(
            inner.config.clone(),
            inner.secrets.clone(),
            self.status.clone(),
            cancel.clone(),
        ));
        inner.active = Some(Active { cancel, task });
    }
}
#[tauri::command]
async fn settings_get(agent: tauri::State<'_, Agent>) -> Result<Settings, String> {
    let mut inner = agent.inner.lock().await;
    if inner.load_failed {
        if let Some((config, secrets)) = agent.store.load()? {
            inner.config = config;
            inner.secrets = secrets;
        }
        inner.load_failed = false;
        if let Ok(mut status) = agent.status.lock() {
            status.message = "设置已恢复，请启动采集".into();
        }
    }
    Ok(Settings::masked(inner.config.clone(), &inner.secrets))
}
#[tauri::command]
async fn settings_save(
    mut settings: Settings,
    agent: tauri::State<'_, Agent>,
) -> Result<Settings, String> {
    let mut inner = agent.inner.lock().await;
    let secrets = settings.secrets(&inner.secrets);
    settings.config.shortcuts_revision = inner
        .config
        .shortcuts_revision
        .saturating_add(1)
        .max(chrono::Utc::now().timestamp_millis() as u64);
    agent.store.save(&settings.config, &secrets)?;
    Agent::stop(&mut inner).await;
    inner.load_failed = false;
    inner.config = settings.config;
    inner.secrets = secrets;
    agent.launch(&mut inner);
    Ok(Settings::masked(inner.config.clone(), &inner.secrets))
}
#[tauri::command]
async fn agent_set_running(running: bool, agent: tauri::State<'_, Agent>) -> Result<(), String> {
    let mut inner = agent.inner.lock().await;
    Agent::stop(&mut inner).await;
    if running {
        inner.config.validate(&inner.secrets)?;
        agent.launch(&mut inner)
    }
    Ok(())
}
#[tauri::command]
fn agent_status(agent: tauri::State<'_, Agent>) -> runtime::Status {
    runtime::snapshot(&agent.status)
}
#[tauri::command]
async fn connection_test(
    settings: Settings,
    agent: tauri::State<'_, Agent>,
) -> Result<String, String> {
    let secrets = {
        let inner = agent.inner.lock().await;
        settings.secrets(&inner.secrets)
    };
    runtime::test_connection(&settings.config, &secrets).await
}
#[tauri::command]
async fn settings_import(path: String) -> Result<Settings, String> {
    tokio::task::spawn_blocking(move || config::import_yaml(std::path::Path::new(&path)))
        .await
        .map_err(|_| "导入失败".to_string())?
}
#[tauri::command]
async fn shortcut_import(path: String) -> Result<shortcuts::Shortcut, String> {
    tokio::task::spawn_blocking(move || shortcuts::import_app(&path))
        .await
        .map_err(|_| "读取应用失败".to_string())?
}
#[tauri::command]
async fn shortcut_icon(path: String) -> Result<String, String> {
    tokio::task::spawn_blocking(move || shortcuts::import_icon(&path))
        .await
        .map_err(|_| "读取图标失败".to_string())?
}
#[tauri::command]
async fn shortcut_website_icon(url: String) -> Result<website_icon::WebsiteInfo, String> {
    website_icon::fetch(&url).await
}
#[tauri::command]
async fn shortcut_recommendations(
    excluded: Vec<String>,
    known_icons: std::collections::HashMap<String, String>,
    recent: tauri::State<'_, Arc<recent_apps::RecentApps>>,
) -> Result<recent_apps::Recommendations, String> {
    let recent = recent.inner().clone();
    tokio::task::spawn_blocking(move || recent.recommendations(&excluded, &known_icons))
        .await
        .map_err(|_| "读取最近应用失败".to_string())?
}
#[tauri::command]
async fn shortcut_preview(item: shortcuts::Shortcut) -> Result<(), String> {
    shortcuts::validate(std::slice::from_ref(&item))?;
    tokio::time::timeout(
        std::time::Duration::from_secs(5),
        shortcuts::open_shortcut(item),
    )
    .await
    .map_err(|_| "打开超时，请重试".to_string())?
    .map_err(|_| "未能打开，请检查应用或网址".to_string())
}
#[tauri::command]
fn autostart_get(app: tauri::AppHandle) -> Result<bool, String> {
    app.autolaunch()
        .is_enabled()
        .map_err(|_| "无法读取开机启动设置".into())
}
#[tauri::command]
fn autostart_set(enabled: bool, app: tauri::AppHandle) -> Result<(), String> {
    let result = if enabled {
        app.autolaunch().enable()
    } else {
        app.autolaunch().disable()
    };
    result.map_err(|_| "无法修改开机启动设置".into())
}
fn show(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
        let _ = window.emit("orbit:ui-visible", true);
    }
}
#[tauri::command]
fn update_status(updater: tauri::State<'_, update::Updater>) -> update::Status {
    updater.snapshot()
}
#[tauri::command]
async fn update_check(
    updater: tauri::State<'_, update::Updater>,
) -> Result<update::Status, String> {
    updater.check().await
}
#[tauri::command]
async fn update_install(
    app: tauri::AppHandle,
    updater: tauri::State<'_, update::Updater>,
) -> Result<update::Status, String> {
    updater.install(app).await
}
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _, _| show(app)))
        .plugin(
            tauri_plugin_autostart::Builder::new()
                .args(["--background"])
                .build(),
        )
        .plugin(tauri_plugin_dialog::init())
        .invoke_handler(tauri::generate_handler![
            settings_get,
            settings_save,
            settings_import,
            shortcut_import,
            shortcut_icon,
            shortcut_website_icon,
            shortcut_recommendations,
            shortcut_preview,
            agent_status,
            agent_set_running,
            connection_test,
            autostart_get,
            autostart_set,
            update_status,
            update_check,
            update_install
        ])
        .setup(|app| {
            app.manage(update::Updater::new(app.package_info().version.to_string()));
            let handle = app.handle().clone();
            tauri::async_runtime::spawn(async move {
                loop {
                    let _ = handle.state::<update::Updater>().check().await;
                    tokio::time::sleep(std::time::Duration::from_secs(6 * 60 * 60)).await;
                }
            });
            let recent =
                recent_apps::RecentApps::new(app.path().app_config_dir()?.join("recent-apps.json"));
            app.manage(recent.clone());
            recent_apps::start(app.handle().clone(), recent);
            let store = Store {
                path: app.path().app_config_dir()?.join("settings.json"),
            };
            let loaded = store.load();
            let ready = matches!(&loaded, Ok(Some(_)));
            let error = loaded.as_ref().err().cloned();
            let load_failed = error.is_some();
            let (config, secrets) = loaded
                .ok()
                .flatten()
                .unwrap_or_else(|| (Config::default(), Secrets::default()));
            let status = Arc::new(Mutex::new(runtime::Status {
                message: error.unwrap_or_else(|| "请完成连接设置".into()),
                ..Default::default()
            }));
            app.manage(Agent {
                inner: AsyncMutex::new(Inner {
                    config,
                    secrets,
                    active: None,
                    load_failed,
                }),
                store,
                status,
            });
            let settings = MenuItem::with_id(app, "settings", "打开设置", true, None::<&str>)?;
            let pause = MenuItem::with_id(app, "toggle", "启动 / 暂停采集", true, None::<&str>)?;
            let quit = MenuItem::with_id(app, "quit", "退出 Orbit Agent", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&settings, &pause, &quit])?;
            TrayIconBuilder::new()
                .icon(tauri::include_image!("icons/tray.png"))
                .icon_as_template(true)
                .tooltip("Orbit Agent")
                .menu(&menu)
                .show_menu_on_left_click(true)
                .on_menu_event(|app, event| match event.id.as_ref() {
                    "settings" => show(app),
                    "toggle" => {
                        let handle = app.clone();
                        tauri::async_runtime::spawn(async move {
                            let agent = handle.state::<Agent>();
                            let mut inner = agent.inner.lock().await;
                            let running = runtime::snapshot(&agent.status).running;
                            Agent::stop(&mut inner).await;
                            if !running {
                                if let Err(e) = inner.config.validate(&inner.secrets) {
                                    if let Ok(mut s) = agent.status.lock() {
                                        s.message = e;
                                    }
                                    show(&handle);
                                } else {
                                    agent.launch(&mut inner);
                                }
                            }
                        });
                    }
                    "quit" => {
                        let handle = app.clone();
                        tauri::async_runtime::spawn(async move {
                            let agent = handle.state::<Agent>();
                            Agent::stop(&mut *agent.inner.lock().await).await;
                            handle.exit(0);
                        });
                    }
                    _ => {}
                })
                .build(app)?;
            if ready {
                let handle = app.handle().clone();
                tauri::async_runtime::spawn(async move {
                    let agent = handle.state::<Agent>();
                    let mut inner = agent.inner.lock().await;
                    match inner.config.validate(&inner.secrets) {
                        Ok(()) => agent.launch(&mut inner),
                        Err(e) => {
                            if let Ok(mut status) = agent.status.lock() {
                                status.message = e
                            }
                        }
                    }
                });
                if std::env::args().any(|s| s == "--background") {
                    if let Some(window) = app.get_webview_window("main") {
                        let _ = window.hide();
                    }
                }
            }
            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
                let _ = window.emit("orbit:ui-visible", false);
            } else if matches!(
                event,
                tauri::WindowEvent::Focused(_) | tauri::WindowEvent::Resized(_)
            ) {
                let visible =
                    window.is_visible().unwrap_or(false) && !window.is_minimized().unwrap_or(false);
                let _ = window.emit("orbit:ui-visible", visible);
            }
        })
        .build(tauri::generate_context!())
        .expect("无法启动 Orbit Agent")
        .run(|app, event| {
            #[cfg(target_os = "macos")]
            if let tauri::RunEvent::Reopen { .. } = event {
                show(app);
            }
            #[cfg(not(target_os = "macos"))]
            let _ = (app, event);
        });
}
