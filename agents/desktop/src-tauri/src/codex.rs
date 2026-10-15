use crate::{
    config::{self, Codex},
    wire::{CodexObservation, CodexSession, CodexWeeklyLimit},
};
use chrono::DateTime;
use rusqlite::{Connection, OpenFlags};
use serde_json::Value;
use std::{
    collections::{HashMap, HashSet},
    fs,
    io::{Read, Seek, SeekFrom},
    path::{Path, PathBuf},
    time::Duration,
};
use tokio::io::{AsyncBufReadExt, AsyncReadExt, AsyncWriteExt, BufReader};

pub fn ts(seconds: i64) -> Option<prost_types::Timestamp> {
    Some(prost_types::Timestamp { seconds, nanos: 0 })
}
pub fn bounded(value: &str, max: usize) -> String {
    let mut end = value.len().min(max);
    while !value.is_char_boundary(end) {
        end -= 1;
    }
    value[..end].to_string()
}
fn clean(s: String) -> String {
    s.split_whitespace().collect::<Vec<_>>().join(" ")
}
fn status(s: &str) -> i32 {
    match s.trim().to_lowercase().replace('_', "").as_str() {
        "running" | "inprogress" => 2,
        "completed" => 3,
        "failed" | "error" => 4,
        "interrupted" => 5,
        "cancelled" | "canceled" => 6,
        _ => 1,
    }
}
#[derive(Clone, Default)]
struct Turn {
    id: String,
    state: i32,
    start: Option<i64>,
    end: Option<i64>,
}
struct Record {
    session: CodexSession,
    cwd: String,
    source: String,
    rollout: String,
}
fn newest(home: &Path, prefix: &str) -> Result<PathBuf, String> {
    let mut files: Vec<_> = fs::read_dir(home)
        .map_err(|_| "找不到 Codex 数据目录")?
        .filter_map(Result::ok)
        .filter(|e| {
            let name = e.file_name();
            let n = name.to_string_lossy();
            n.starts_with(prefix) && n.ends_with(".sqlite")
        })
        .filter_map(|e| {
            e.metadata()
                .ok()
                .filter(|m| m.is_file())
                .map(|m| (m.modified().ok(), e.path()))
        })
        .collect();
    files.sort_by(|a, b| b.0.cmp(&a.0).then_with(|| a.1.cmp(&b.1)));
    files
        .first()
        .map(|x| x.1.clone())
        .ok_or_else(|| "Codex 数据库尚未生成".into())
}
fn database(path: &Path) -> Result<Connection, String> {
    let c = Connection::open_with_flags(
        path,
        OpenFlags::SQLITE_OPEN_READ_ONLY | OpenFlags::SQLITE_OPEN_NO_MUTEX,
    )
    .map_err(|_| "无法读取 Codex 数据库")?;
    c.busy_timeout(Duration::from_secs(2))
        .map_err(|_| "无法读取 Codex 数据库")?;
    Ok(c)
}
fn text(row: &rusqlite::Row<'_>, i: usize) -> rusqlite::Result<String> {
    Ok(row.get::<_, Option<String>>(i)?.unwrap_or_default())
}
fn records(db: &Connection, archived: bool) -> Result<Vec<Record>, String> {
    let columns: HashSet<String> = db
        .prepare("PRAGMA table_info(threads)")
        .map_err(|_| "会话数据库结构不兼容")?
        .query_map([], |r| r.get(1))
        .map_err(|_| "会话数据库结构不兼容")?
        .collect::<Result<_, _>>()
        .map_err(|_| "会话数据库结构不兼容")?;
    let name = if columns.contains("name") {
        "name"
    } else {
        "NULL"
    };
    let rollout = if columns.contains("rollout_path") {
        "rollout_path"
    } else {
        "NULL"
    };
    let edges:bool=db.query_row("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='thread_spawn_edges')",[],|r|r.get(0)).map_err(|_|"无法读取会话数据")?;
    let spawned = if edges {
        "AND NOT EXISTS (SELECT 1 FROM thread_spawn_edges WHERE child_thread_id=threads.id)"
    } else {
        ""
    };
    let query=format!("SELECT id,{name},title,source,cwd,model,updated_at_ms,first_user_message,{rollout} FROM threads WHERE {} {spawned} ORDER BY updated_at_ms DESC,id",if archived{"1"}else{"archived=0"});
    let mut stmt = db.prepare(&query).map_err(|_| "会话数据库结构不兼容")?;
    let result = stmt
        .query_map([], |r| {
            let title = [text(r, 1)?, text(r, 2)?, text(r, 7)?]
                .into_iter()
                .map(clean)
                .find(|s| !s.is_empty())
                .unwrap_or_else(|| "(无标题)".into());
            let cwd = text(r, 4)?;
            let project = cwd
                .trim_end_matches(['/', '\\'])
                .rsplit(['/', '\\'])
                .next()
                .unwrap_or("")
                .to_string();
            Ok(Record {
                session: CodexSession {
                    session_id: text(r, 0)?,
                    display_name: title,
                    project_name: project,
                    model: text(r, 5)?,
                    status: 1,
                    updated_at: ts(r.get::<_, Option<i64>>(6)?.unwrap_or(0) / 1000),
                    process_alive: false,
                },
                cwd,
                source: text(r, 3)?,
                rollout: text(r, 8)?,
            })
        })
        .map_err(|_| "无法读取会话数据")?
        .collect::<Result<Vec<_>, _>>()
        .map_err(|_| "无法读取会话数据")?;
    Ok(result)
}
fn turns(db: &Connection) -> Result<HashMap<String, Turn>, String> {
    let mut stmt=db.prepare("SELECT thread_id,turn_id,status,started_at,completed_at FROM (SELECT thread_id,turn_id,status,started_at,completed_at,ROW_NUMBER() OVER (PARTITION BY thread_id ORDER BY COALESCE(started_at,0) DESC,rowid DESC) rank FROM thread_turns) WHERE rank=1").map_err(|_|"会话历史结构不兼容")?;
    let result = stmt
        .query_map([], |r| {
            Ok((
                text(r, 0)?,
                Turn {
                    id: text(r, 1)?,
                    state: status(&text(r, 2)?),
                    start: r.get(3)?,
                    end: r.get(4)?,
                },
            ))
        })
        .map_err(|_| "无法读取会话历史")?
        .collect::<Result<_, _>>()
        .map_err(|_| "无法读取会话历史".into());
    result
}
fn processes(home: &Path) -> HashSet<String> {
    let value = fs::read(home.join("process_manager/chat_processes.json"))
        .ok()
        .and_then(|b| serde_json::from_slice::<Value>(&b).ok())
        .unwrap_or(Value::Null);
    let entries: Vec<_> = value
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(|v| {
            let raw = &v["osPid"];
            let pid = raw.as_u64().or_else(|| raw.as_str()?.parse().ok())?;
            if pid == 0 || pid > u32::MAX as u64 {
                return None;
            }
            Some((
                v["conversationId"].as_str()?.to_string(),
                sysinfo::Pid::from_u32(pid as u32),
            ))
        })
        .collect();
    let ids: Vec<_> = entries.iter().map(|(_, p)| *p).collect();
    let mut system = sysinfo::System::new();
    system.refresh_processes(sysinfo::ProcessesToUpdate::Some(&ids), true);
    entries
        .into_iter()
        .filter(|(_, p)| system.process(*p).is_some())
        .map(|(id, _)| id)
        .collect()
}
fn rollout(path: &str) -> Option<Turn> {
    let mut file = fs::File::open(path).ok()?;
    let len = file.metadata().ok()?.len();
    let start = len.saturating_sub(1 << 20);
    file.seek(SeekFrom::Start(start.saturating_sub(1))).ok()?;
    let mut bytes = vec![];
    std::io::Read::take(file, (1 << 20) + 1)
        .read_to_end(&mut bytes)
        .ok()?;
    let content = if start > 0 {
        let prev = bytes.first().copied()?;
        let rest = &bytes[1..];
        if prev != b'\n' {
            let split = rest.iter().position(|c| *c == b'\n')?;
            &rest[split + 1..]
        } else {
            rest
        }
    } else {
        &bytes[..]
    };
    let mut latest: Option<Turn> = None;
    for line in content.split(|b| *b == b'\n') {
        let Ok(v) = serde_json::from_slice::<Value>(line) else {
            continue;
        };
        if v["type"] != "event_msg" {
            continue;
        }
        let p = &v["payload"];
        let state = match p["type"].as_str() {
            Some("task_started") => 2,
            Some("task_complete") => 3,
            Some("turn_aborted") => status(p["reason"].as_str().unwrap_or("interrupted")),
            _ => continue,
        };
        let id = p["turn_id"].as_str().unwrap_or("").to_string();
        let timestamp = v["timestamp"]
            .as_str()
            .and_then(|t| DateTime::parse_from_rfc3339(t).ok())
            .map(|t| t.timestamp());
        let mut start = p["started_at"]
            .as_i64()
            .or(if state == 2 { timestamp } else { None });
        let end = p["completed_at"]
            .as_i64()
            .or(if state != 2 { timestamp } else { None });
        if start.is_none() && !id.is_empty() {
            if let Some(previous) = &latest {
                if previous.id == id {
                    start = previous.start;
                }
            }
        }
        latest = Some(Turn {
            id,
            state,
            start,
            end,
        });
    }
    latest
}
fn newer(a: &Turn, b: &Turn) -> bool {
    if let Some(start) = a.start {
        return b.start.is_none()
            || Some(start) > b.start
            || (Some(start) == b.start
                && !a.id.is_empty()
                && (a.id != b.id || a.state != b.state));
    }
    a.state != 1
        && a.state != 2
        && ((!a.id.is_empty() && a.id == b.id && a.state != b.state)
            || (a.end.is_some() && b.start.is_some() && a.end > b.start))
}
pub fn collect(c: &Codex, now: i64) -> Result<CodexObservation, String> {
    let home = config::codex_home(&c.home);
    let db = database(&newest(&home, "state_")?)?;
    let history = database(&newest(&home, "thread_history_")?)?;
    let turns = turns(&history)?;
    let alive = processes(&home);
    let mut all = records(&db, c.include_archived)?;
    all.retain(|r| {
        let sub = serde_json::from_str::<Value>(&r.source)
            .ok()
            .and_then(|v| v.as_object().map(|m| m.contains_key("subagent")))
            .unwrap_or(false);
        !sub && !r
            .cwd
            .replace('\\', "/")
            .trim_end_matches('/')
            .ends_with("/Library/Application Support/Dayflow/chatcli")
            && !c
                .ignore_cwd
                .iter()
                .any(|p| !p.trim().is_empty() && r.cwd.contains(p))
            && !c.ignore_source.contains(&r.source)
    });
    for (index, r) in all.iter_mut().enumerate() {
        if let Some(turn) = turns.get(&r.session.session_id) {
            r.session.status = turn.state;
            r.session.process_alive = alive.contains(&r.session.session_id);
            let updated = r
                .session
                .updated_at
                .as_ref()
                .map(|t| t.seconds)
                .unwrap_or(0);
            if (index < c.limit || turn.state == 2)
                && (turn
                    .end
                    .map(|end| updated - end >= 2)
                    .unwrap_or(turn.state == 2 && !r.session.process_alive))
            {
                if let Some(candidate) = rollout(&r.rollout) {
                    if newer(&candidate, turn) {
                        r.session.status = candidate.state;
                    }
                }
                let mtime = fs::metadata(&r.rollout)
                    .ok()
                    .and_then(|m| m.modified().ok())
                    .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
                    .map(|d| d.as_secs() as i64)
                    .unwrap_or(0);
                if r.session.status == 2
                    && !r.session.process_alive
                    && now - updated.max(turn.start.unwrap_or(0)).max(mtime) > 86400
                {
                    r.session.status = 1;
                }
            }
        }
    }
    let running = all.iter().filter(|r| r.session.status == 2).count();
    let total = all.len();
    let sessions = all
        .into_iter()
        .take(c.limit)
        .map(|r| {
            let mut s = r.session;
            s.session_id = bounded(&s.session_id, 128);
            s.display_name = if c.include_title {
                bounded(&s.display_name, 192)
            } else {
                String::new()
            };
            s.project_name = if c.include_project {
                bounded(&s.project_name, 96)
            } else {
                String::new()
            };
            s.model = bounded(&s.model, 64);
            s
        })
        .collect();
    Ok(CodexObservation {
        sessions,
        total_count: total as u32,
        running_count: running as u32,
        observed_at: ts(now),
        weekly_limit: None,
    })
}

pub fn parse_weekly(v: &Value, now: i64) -> Result<CodexWeeklyLimit, String> {
    let bucket = if v.get("rateLimitsByLimitId").is_some_and(|x| !x.is_null()) {
        &v["rateLimitsByLimitId"]["codex"]
    } else {
        &v["rateLimits"]
    };
    if bucket.is_null()
        || bucket["limitId"]
            .as_str()
            .is_some_and(|id| !id.is_empty() && id != "codex")
    {
        return Err("Codex 周限不可用".into());
    }
    for key in ["primary", "secondary"] {
        let w = &bucket[key];
        if w["windowDurationMins"] == 10080 {
            let used = w["usedPercent"]
                .as_f64()
                .filter(|n| n.is_finite() && *n >= 0.)
                .ok_or("周限数据格式不正确")?;
            let resets = w["resetsAt"]
                .as_i64()
                .filter(|n| *n > now)
                .ok_or("周限数据已过期")?;
            return Ok(CodexWeeklyLimit {
                remaining_percent: (100. - used).max(0.),
                resets_at: ts(resets),
                observed_at: ts(now),
                fresh_until: ts(now + 180),
            });
        }
    }
    Err("Codex 周限不可用".into())
}
pub async fn weekly(c: &Codex, now: i64) -> Result<CodexWeeklyLimit, String> {
    let mut command = tokio::process::Command::new(&c.binary);
    command
        .args(["app-server", "--listen", "stdio://"])
        .env("CODEX_HOME", config::codex_home(&c.home))
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::null())
        .kill_on_drop(true);
    #[cfg(windows)]
    command.creation_flags(0x08000000);
    let mut child = command
        .spawn()
        .map_err(|_| "无法启动 Codex，请检查程序位置")?;
    let result=tokio::time::timeout(Duration::from_secs(15),async{
        let mut input=child.stdin.take().ok_or("无法连接 Codex")?;let mut output=BufReader::new(child.stdout.take().ok_or("无法连接 Codex")?);
        input.write_all(b"{\"id\":1,\"method\":\"initialize\",\"params\":{\"clientInfo\":{\"name\":\"orbit\",\"title\":\"Orbit\",\"version\":\"0.1.0\"}}}\n").await.map_err(|_|"无法初始化 Codex")?;
        let mut initialized=false;
        loop{let mut bytes=Vec::new();let n=(&mut output).take(1048577).read_until(b'\n',&mut bytes).await.map_err(|_|"读取 Codex 周限失败")?;if n==0||n>1048576{return Err("Codex 返回数据不可用".into())}
            let Ok(v)=serde_json::from_slice::<Value>(&bytes)else{continue};let id=v["id"].as_i64().unwrap_or(0);if id!=1&&id!=2{continue}
            if !v["error"].is_null(){return Err("请在 Codex 中登录后重试".into())}
            if id==2{return parse_weekly(&v["result"],now)}
            if !initialized{initialized=true;input.write_all(b"{\"method\":\"initialized\"}\n{\"id\":2,\"method\":\"account/rateLimits/read\"}\n").await.map_err(|_|"无法读取 Codex 周限")?;}
        }
    }).await.map_err(|_|"读取 Codex 周限超时".to_string()).and_then(|r|r);
    let _ = child.kill().await;
    let _ = child.wait().await;
    result
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn windows_project_and_filters() {
        assert_eq!("C:\\work\\orbit".rsplit(['/', '\\']).next(), Some("orbit"));
        assert_eq!(status("in_progress"), 2);
        assert_eq!(bounded("中文abc", 7), "中文a");
    }
    #[test]
    fn weekly_selects_codex_and_rejects_missing_bucket() {
        let v = serde_json::json!({"rateLimitsByLimitId":{"codex":{"secondary":{"windowDurationMins":10080,"usedPercent":23.5,"resetsAt":200}}}});
        assert_eq!(parse_weekly(&v, 100).unwrap().remaining_percent, 76.5);
        assert!(parse_weekly(&serde_json::json!({"rateLimitsByLimitId":{},"rateLimits":{"secondary":{"windowDurationMins":10080,"usedPercent":0,"resetsAt":200}}}),100).is_err());
    }
    #[test]
    fn same_turn_completion_reconciles() {
        let old = Turn {
            id: "a".into(),
            state: 2,
            start: Some(100),
            end: None,
        };
        assert!(newer(
            &Turn {
                id: "a".into(),
                state: 3,
                start: None,
                end: Some(101)
            },
            &old
        ));
        assert!(!newer(
            &Turn {
                id: "b".into(),
                state: 2,
                start: Some(99),
                end: None
            },
            &old
        ));
    }
    #[test]
    fn sqlite_collection_filters_and_redacts() {
        let dir = tempfile::tempdir().unwrap();
        let db = Connection::open(dir.path().join("state_1.sqlite")).unwrap();
        db.execute_batch("CREATE TABLE threads(id TEXT,name TEXT,title TEXT,source TEXT,cwd TEXT,model TEXT,updated_at_ms INTEGER,first_user_message TEXT,archived INTEGER,rollout_path TEXT);INSERT INTO threads VALUES('a','Name','Title','cli','C:\\work\\orbit','gpt',100000,'First',0,NULL),('b','Hidden','', '{\"subagent\":{}}','/work','gpt',100000,'',0,NULL);").unwrap();
        let history = Connection::open(dir.path().join("thread_history_1.sqlite")).unwrap();
        history.execute_batch("CREATE TABLE thread_turns(thread_id TEXT,turn_id TEXT,status TEXT,started_at INTEGER,completed_at INTEGER);INSERT INTO thread_turns VALUES('a','turn','in_progress',100,NULL);").unwrap();
        let mut c = Codex {
            home: dir.path().display().to_string(),
            ..Codex::default()
        };
        let result = collect(&c, 101).unwrap();
        assert_eq!(result.total_count, 1);
        assert_eq!(result.running_count, 1);
        assert_eq!(result.sessions[0].display_name, "");
        c.include_title = true;
        c.include_project = true;
        let result = collect(&c, 101).unwrap();
        assert_eq!(result.sessions[0].display_name, "Name");
        assert_eq!(result.sessions[0].project_name, "orbit");
    }
}
