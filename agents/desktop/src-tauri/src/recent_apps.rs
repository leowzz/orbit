//! A bounded, local-only record of application launches and foreground switches.
#![cfg_attr(not(any(target_os = "macos", windows)), allow(dead_code))]
use crate::shortcuts::{import_app, Shortcut};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{
    collections::{HashMap, HashSet},
    path::PathBuf,
    sync::{Arc, Mutex},
};

const LIMIT: usize = 64;
const RETENTION: i64 = 90 * 24 * 3600;

fn path_key(path: &str) -> String {
    #[cfg(windows)]
    {
        crate::windows_apps::path_key(path)
    }
    #[cfg(not(windows))]
    {
        path.to_string()
    }
}

#[derive(Clone, Serialize, Deserialize, PartialEq, Debug)]
#[serde(rename_all = "camelCase")]
struct Entry {
    path: String,
    last_used: i64,
    last_launched: i64,
}
#[derive(Default)]
struct History {
    entries: Vec<Entry>,
    foreground: Option<String>,
    running: HashSet<String>,
}
impl History {
    fn observe(&mut self, running: &[(String, i64)], front: Option<String>, now: i64) -> bool {
        let before = self.entries.clone();
        self.running = running.iter().map(|(path, _)| path_key(path)).collect();
        let front = front.map(|path| path_key(&path));
        for (path, launched) in running {
            let valid_launch = *launched > 0 && *launched <= now && now - launched <= RETENTION;
            if let Some(entry) = self
                .entries
                .iter_mut()
                .find(|e| path_key(&e.path) == path_key(path))
            {
                if valid_launch && *launched > entry.last_launched {
                    entry.last_launched = *launched;
                    entry.last_used = entry.last_used.max(*launched);
                }
                // Keep a long-running application discoverable even without a launch timestamp.
                if now - entry.last_used > RETENTION {
                    entry.last_used = now;
                }
            } else {
                self.entries.push(Entry {
                    path: path.clone(),
                    last_used: if valid_launch { *launched } else { now },
                    last_launched: if valid_launch { *launched } else { 0 },
                });
            }
        }
        if front != self.foreground {
            if let Some(path) = &front {
                if let Some(entry) = self.entries.iter_mut().find(|e| path_key(&e.path) == *path) {
                    entry.last_used = now;
                } else {
                    self.entries.push(Entry {
                        path: path.clone(),
                        last_used: now,
                        last_launched: 0,
                    });
                }
            }
            self.foreground = front;
        }
        self.entries
            .retain(|e| now - e.last_used <= RETENTION && e.last_used <= now);
        self.entries.sort_by(|a, b| {
            b.last_used
                .cmp(&a.last_used)
                .then_with(|| a.path.cmp(&b.path))
        });
        self.entries.truncate(LIMIT);
        before != self.entries
    }

    fn status(&self, path: &str) -> AppStatus {
        let key = path_key(path);
        if self.foreground.as_ref() == Some(&key) {
            AppStatus::Foreground
        } else if self.running.contains(&key) {
            AppStatus::Running
        } else {
            AppStatus::Recent
        }
    }

    fn ranked(&self) -> Vec<(Entry, AppStatus)> {
        let mut entries: Vec<_> = self
            .entries
            .iter()
            .map(|entry| (entry.clone(), self.status(&entry.path)))
            .collect();
        entries.sort_by(|(a, a_status), (b, b_status)| {
            a_status
                .cmp(b_status)
                .then_with(|| b.last_used.cmp(&a.last_used))
                .then_with(|| a.path.cmp(&b.path))
        });
        entries
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord, Serialize)]
#[serde(rename_all = "camelCase")]
enum AppStatus {
    Foreground,
    Running,
    Recent,
}

pub struct RecentApps {
    path: PathBuf,
    history: Mutex<History>,
    cache: Mutex<HashMap<String, Shortcut>>,
    error: Mutex<Option<String>>,
}
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Recommendation {
    app: Shortcut,
    last_used: i64,
    icon_key: String,
    status: AppStatus,
}
#[derive(Serialize)]
pub struct Recommendations {
    supported: bool,
    items: Vec<Recommendation>,
}
impl RecentApps {
    pub fn new(path: PathBuf) -> Arc<Self> {
        let loaded = match std::fs::read(&path) {
            Ok(bytes) => serde_json::from_slice::<Vec<Entry>>(&bytes)
                .map_err(|_| "最近应用记录无法读取，请检查本机记录文件".to_string()),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(Vec::new()),
            Err(_) => Err("最近应用记录无法读取，请检查目录权限".into()),
        };
        let error = loaded.as_ref().err().cloned();
        Arc::new(Self {
            path,
            history: Mutex::new(History {
                entries: loaded.unwrap_or_default(),
                foreground: None,
                running: HashSet::new(),
            }),
            cache: Mutex::new(HashMap::new()),
            error: Mutex::new(error),
        })
    }
    #[cfg(any(target_os = "macos", windows))]
    fn observe(&self, running: Vec<(String, i64)>, front: Option<String>) {
        // Never overwrite a damaged history with an empty one.
        if self.error.lock().unwrap().is_some() {
            return;
        }
        let mut h = self.history.lock().unwrap();
        if h.observe(&running, front, chrono::Utc::now().timestamp())
            && self.save(&h.entries).is_err()
        {
            *self.error.lock().unwrap() = Some("最近应用记录无法保存，请检查目录权限".into());
        }
    }
    fn save(&self, entries: &[Entry]) -> Result<(), std::io::Error> {
        use std::io::Write;
        if let Some(parent) = self.path.parent() {
            std::fs::create_dir_all(parent)?;
        }
        let temp = self.path.with_extension("tmp");
        let mut options = std::fs::OpenOptions::new();
        options.write(true).create(true).truncate(true);
        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt;
            options.mode(0o600);
        }
        let mut file = options.open(&temp)?;
        file.write_all(&serde_json::to_vec(entries)?)?;
        file.sync_all()?;
        drop(file);
        crate::config::replace_file(&temp, &self.path)
    }
    pub fn recommendations(
        &self,
        excluded: &[String],
        known_icons: &HashMap<String, String>,
    ) -> Result<Recommendations, String> {
        if let Some(error) = self.error.lock().unwrap().clone() {
            return Err(error);
        }
        let entries = self.history.lock().unwrap().ranked();
        let excluded: HashSet<_> = excluded.iter().map(|path| path_key(path)).collect();
        let mut cache = self.cache.lock().unwrap();
        cache.retain(|path, _| entries.iter().any(|(e, _)| &e.path == path));
        let mut items = Vec::new();
        let mut recent_count = 0;
        for (entry, status) in entries {
            // Running apps must not consume the slots for applications used earlier.
            if status == AppStatus::Recent && recent_count >= 12 {
                break;
            }
            if excluded.contains(&path_key(&entry.path))
                || !crate::shortcuts::app_path(std::path::Path::new(&entry.path))
            {
                continue;
            }
            if !cache.contains_key(&entry.path) {
                if let Ok(app) = import_app(&entry.path) {
                    cache.insert(entry.path.clone(), app);
                }
            }
            if let Some(app) = cache.get(&entry.path) {
                if !excluded.contains(&path_key(&app.target)) {
                    items.push(Recommendation::new(
                        app,
                        entry.last_used,
                        status,
                        known_icons,
                    ));
                    if status == AppStatus::Recent {
                        recent_count += 1;
                    }
                }
            }
        }
        Ok(Recommendations {
            supported: cfg!(any(target_os = "macos", windows)),
            items,
        })
    }
}

impl Recommendation {
    fn new(
        app: &Shortcut,
        last_used: i64,
        status: AppStatus,
        known_icons: &HashMap<String, String>,
    ) -> Self {
        let icon_key = hex::encode(Sha256::digest(app.icon.as_bytes()));
        let mut app = app.clone();
        if known_icons.get(&app.target) == Some(&icon_key) {
            app.icon.clear();
        }
        Self {
            app,
            last_used,
            icon_key,
            status,
        }
    }
}

#[cfg(target_os = "macos")]
fn sample() -> (Vec<(String, i64)>, Option<String>) {
    use objc2_app_kit::{NSApplicationActivationPolicy, NSRunningApplication, NSWorkspace};
    fn path(app: &NSRunningApplication) -> Option<String> {
        if app.activationPolicy() != NSApplicationActivationPolicy::Regular
            || app
                .bundleIdentifier()
                .is_some_and(|id| id.to_string() == "dev.orbit.agent")
        {
            return None;
        }
        let path = app.bundleURL()?.path()?.to_string();
        crate::shortcuts::app_path(std::path::Path::new(&path)).then_some(path)
    }
    objc2::rc::autoreleasepool(|_| {
        let workspace = NSWorkspace::sharedWorkspace();
        let running = workspace
            .runningApplications()
            .iter()
            .filter_map(|app| {
                Some((
                    path(&app)?,
                    app.launchDate()
                        .map(|d| d.timeIntervalSince1970() as i64)
                        .unwrap_or(0),
                ))
            })
            .collect();
        let front = workspace.frontmostApplication().and_then(|app| path(&app));
        (running, front)
    })
}

pub fn start(app: tauri::AppHandle, recent: Arc<RecentApps>) {
    #[cfg(windows)]
    {
        let _ = app;
        tauri::async_runtime::spawn(async move {
            loop {
                let recent = recent.clone();
                if tauri::async_runtime::spawn_blocking(move || {
                    // A transient enumeration failure must not mark every application closed.
                    if let Ok((running, front)) = crate::windows_apps::sample() {
                        recent.observe(running, front);
                    }
                })
                .await
                .is_err()
                {
                    break;
                }
                tokio::time::sleep(std::time::Duration::from_secs(2)).await;
            }
        });
    }
    #[cfg(target_os = "macos")]
    tauri::async_runtime::spawn(async move {
        loop {
            let (send, receive) = tokio::sync::oneshot::channel();
            if app
                .run_on_main_thread(move || {
                    let _ = send.send(sample());
                })
                .is_err()
            {
                break;
            }
            if let Ok((running, front)) = receive.await {
                let recent = recent.clone();
                let _ =
                    tauri::async_runtime::spawn_blocking(move || recent.observe(running, front))
                        .await;
            } else {
                break;
            }
            tokio::time::sleep(std::time::Duration::from_secs(2)).await;
        }
    });
    #[cfg(not(any(target_os = "macos", windows)))]
    let _ = (app, recent);
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn recommendation_icons_are_only_sent_when_not_cached() {
        let mut app = Shortcut {
            id: "app".into(),
            title: "App".into(),
            kind: "app".into(),
            target: "/Applications/App.app".into(),
            icon: "png-v1".into(),
        };
        let first = Recommendation::new(&app, 10, AppStatus::Running, &HashMap::new());
        assert_eq!(first.app.icon, "png-v1");
        let known = HashMap::from([(app.target.clone(), first.icon_key.clone())]);
        let cached = Recommendation::new(&app, 11, AppStatus::Recent, &known);
        assert!(cached.app.icon.is_empty());
        assert_eq!(cached.icon_key, first.icon_key);
        assert_eq!(cached.last_used, 11);
        assert_eq!(cached.status, AppStatus::Recent);
        assert_eq!(app.icon, "png-v1", "cache must retain the original icon");
        app.icon = "png-v2".into();
        let changed = Recommendation::new(&app, 12, AppStatus::Recent, &known);
        assert_eq!(changed.app.icon, "png-v2");
        assert_ne!(changed.icon_key, first.icon_key);
        app.icon.clear();
        assert_ne!(
            Recommendation::new(&app, 13, AppStatus::Recent, &known).icon_key,
            first.icon_key
        );
    }
    #[test]
    fn tracks_switches_and_launches_without_repeated_writes() {
        let mut h = History::default();
        let running = vec![("a".into(), 10), ("b".into(), 20)];
        assert!(h.observe(&running, Some("a".into()), 30));
        assert_eq!(h.entries[0].path, "a");
        assert!(!h.observe(&running, Some("a".into()), 31));
        h.observe(&running, None, 32);
        h.observe(&running, Some("b".into()), 33);
        assert_eq!(h.entries[0].path, "b");
        h.observe(&[("a".into(), 34)], Some("b".into()), 34);
        assert_eq!(h.entries[0].path, "a");
    }
    #[test]
    fn bounds_history_and_persists_without_icons_or_credentials() {
        let mut h = History::default();
        h.observe(
            &(1..100).map(|i| (format!("app{i}"), i)).collect::<Vec<_>>(),
            None,
            100,
        );
        assert_eq!(h.entries.len(), LIMIT);
        let dir = tempfile::tempdir().unwrap();
        let store = RecentApps::new(dir.path().join("recent.json"));
        store.save(&h.entries).unwrap();
        let restored = RecentApps::new(store.path.clone());
        assert_eq!(restored.history.lock().unwrap().entries, h.entries);
        h.observe(&[], None, RETENTION + 200);
        assert!(h.entries.is_empty());
    }

    #[test]
    fn running_apps_rank_first_and_closed_apps_survive_restart_as_recent() {
        let mut h = History::default();
        h.observe(&[("closed".into(), 90)], Some("closed".into()), 95);
        h.observe(
            &[("active".into(), 10), ("running".into(), 20)],
            Some("active".into()),
            100,
        );
        let ranked = h.ranked();
        assert_eq!(
            ranked
                .iter()
                .map(|(e, s)| (e.path.as_str(), *s))
                .collect::<Vec<_>>(),
            [
                ("active", AppStatus::Foreground),
                ("running", AppStatus::Running),
                ("closed", AppStatus::Recent)
            ]
        );
        let dir = tempfile::tempdir().unwrap();
        let store = RecentApps::new(dir.path().join("recent.json"));
        store.save(&h.entries).unwrap();
        // Exercise replacement of the existing file on Windows, not just its first creation.
        h.observe(&[], None, 101);
        store.save(&h.entries).unwrap();
        let restored = RecentApps::new(store.path.clone());
        assert!(restored
            .history
            .lock()
            .unwrap()
            .ranked()
            .iter()
            .all(|(_, s)| *s == AppStatus::Recent));
        assert_eq!(restored.history.lock().unwrap().entries, h.entries);
        let persisted = std::fs::read_to_string(&store.path).unwrap();
        assert!(!persisted.contains("\"status\""));
        assert!(!persisted.contains("foreground"));
    }

    #[test]
    fn long_running_apps_without_recent_launch_times_are_still_discoverable() {
        let mut h = History::default();
        let running = vec![("old".into(), 1), ("unknown".into(), 0)];
        h.observe(&running, None, RETENTION + 100);
        assert_eq!(h.entries.len(), 2);
        assert!(h.ranked().iter().all(|(_, s)| *s == AppStatus::Running));
        assert!(!h.observe(&running, None, RETENTION + 101));
    }

    #[cfg(windows)]
    #[test]
    fn many_running_apps_do_not_hide_recently_closed_apps() {
        let dir = tempfile::tempdir().unwrap();
        let store = RecentApps::new(dir.path().join("recent.json"));
        let now = chrono::Utc::now().timestamp();
        let running: Vec<_> = (0..14)
            .map(|i| {
                let path = dir.path().join(format!("app{i}.exe"));
                std::fs::write(&path, []).unwrap();
                (path.to_string_lossy().to_string(), now - 20)
            })
            .collect();
        store.observe(running.clone(), None);
        store.observe(running[..13].to_vec(), None);
        let recommendations = store.recommendations(&[], &HashMap::new()).unwrap();
        assert_eq!(recommendations.items.len(), 14);
        assert_eq!(
            recommendations
                .items
                .iter()
                .filter(|i| i.status == AppStatus::Running)
                .count(),
            13
        );
        assert_eq!(
            recommendations.items.last().unwrap().status,
            AppStatus::Recent
        );
    }

    #[cfg(windows)]
    #[test]
    fn windows_history_and_exclusions_ignore_path_spelling() {
        let dir = tempfile::tempdir().unwrap();
        let store = RecentApps::new(dir.path().join("recent.json"));
        let exe = std::path::PathBuf::from(std::env::var_os("SystemRoot").unwrap())
            .join("System32/cmd.exe");
        let path = exe.to_string_lossy().to_string();
        let canonical = exe
            .canonicalize()
            .unwrap()
            .to_string_lossy()
            .to_string()
            .to_uppercase();
        let now = chrono::Utc::now().timestamp();
        store.observe(vec![(path.clone(), now - 10)], Some(path));
        store.observe(vec![(canonical.clone(), now - 10)], Some(canonical.clone()));
        assert_eq!(store.history.lock().unwrap().entries.len(), 1);
        assert!(store
            .recommendations(&[canonical], &HashMap::new())
            .unwrap()
            .items
            .is_empty());
    }
}
