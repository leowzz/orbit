//! A bounded, local-only record of application launches and foreground switches.
#![cfg_attr(not(target_os = "macos"), allow(dead_code))]
use crate::shortcuts::{import_app, Shortcut};
use serde::{Deserialize, Serialize};
use std::{
    collections::HashMap,
    path::PathBuf,
    sync::{Arc, Mutex},
};

const LIMIT: usize = 64;
const RETENTION: i64 = 90 * 24 * 3600;

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
}
impl History {
    fn observe(&mut self, running: &[(String, i64)], front: Option<String>, now: i64) -> bool {
        let before = self.entries.clone();
        for (path, launched) in running {
            if *launched <= 0 || *launched > now || now - launched > RETENTION {
                continue;
            }
            if let Some(entry) = self.entries.iter_mut().find(|e| e.path == *path) {
                if *launched > entry.last_launched {
                    entry.last_launched = *launched;
                    entry.last_used = entry.last_used.max(*launched);
                }
            } else {
                self.entries.push(Entry {
                    path: path.clone(),
                    last_used: *launched,
                    last_launched: *launched,
                });
            }
        }
        if front != self.foreground {
            if let Some(path) = &front {
                if let Some(entry) = self.entries.iter_mut().find(|e| e.path == *path) {
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
            }),
            cache: Mutex::new(HashMap::new()),
            error: Mutex::new(error),
        })
    }
    #[cfg(target_os = "macos")]
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
        std::fs::rename(temp, &self.path)
    }
    pub fn recommendations(&self, excluded: &[String]) -> Result<Recommendations, String> {
        if let Some(error) = self.error.lock().unwrap().clone() {
            return Err(error);
        }
        let entries = self.history.lock().unwrap().entries.clone();
        let mut cache = self.cache.lock().unwrap();
        cache.retain(|path, _| entries.iter().any(|e| &e.path == path));
        let mut items = Vec::new();
        for entry in entries {
            if excluded.contains(&entry.path)
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
                if !excluded.contains(&app.target) {
                    items.push(Recommendation {
                        app: app.clone(),
                        last_used: entry.last_used,
                    });
                }
            }
            if items.len() >= 12 {
                break;
            }
        }
        Ok(Recommendations {
            supported: cfg!(target_os = "macos"),
            items,
        })
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
    #[cfg(not(target_os = "macos"))]
    let _ = (app, recent);
}

#[cfg(test)]
mod tests {
    use super::*;
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
}
