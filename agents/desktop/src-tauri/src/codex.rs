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
fn thread_columns(db: &Connection) -> Result<HashSet<String>, String> {
    db.prepare("PRAGMA table_info(threads)")
        .map_err(|_| "会话数据库结构不兼容")?
        .query_map([], |r| r.get(1))
        .map_err(|_| "会话数据库结构不兼容")?
        .collect::<Result<_, _>>()
        .map_err(|_| "会话数据库结构不兼容".into())
}
fn session_titles(db: &Connection, sessions: &mut [CodexSession]) -> Result<(), String> {
    let columns = thread_columns(db)?;
    let name = if columns.contains("name") {
        "name"
    } else {
        "NULL"
    };
    let mut stmt = db
        .prepare(&format!(
            "SELECT {name},title,first_user_message FROM threads WHERE id=?1"
        ))
        .map_err(|_| "会话数据库结构不兼容")?;
    for session in sessions {
        session.display_name = stmt
            .query_row([&session.session_id], |row| {
                for i in 0..3 {
                    let candidate = clean(text(row, i)?);
                    if !candidate.is_empty() {
                        return Ok(bounded(&candidate, 192));
                    }
                }
                Ok("(无标题)".into())
            })
            .map_err(|_| "无法读取会话数据")?;
    }
    Ok(())
}
fn records(db: &Connection, archived: bool) -> Result<Vec<Record>, String> {
    let columns = thread_columns(db)?;
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
    // Titles and first messages can be very large. Keep them out of the full-table
    // scan/sort; session_titles reads them only for the final visible sessions.
    let query=format!("SELECT id,source,cwd,model,updated_at_ms,{rollout} FROM threads WHERE {} {spawned} ORDER BY updated_at_ms DESC,id",if archived{"1"}else{"archived=0"});
    let mut stmt = db.prepare(&query).map_err(|_| "会话数据库结构不兼容")?;
    let result = stmt
        .query_map([], |r| {
            let cwd = text(r, 2)?;
            let project = cwd
                .trim_end_matches(['/', '\\'])
                .rsplit(['/', '\\'])
                .next()
                .unwrap_or("")
                .to_string();
            Ok(Record {
                session: CodexSession {
                    session_id: text(r, 0)?,
                    display_name: String::new(),
                    project_name: project,
                    model: text(r, 3)?,
                    status: 1,
                    updated_at: ts(r.get::<_, Option<i64>>(4)?.unwrap_or(0) / 1000),
                    process_alive: false,
                },
                cwd,
                source: text(r, 1)?,
                rollout: text(r, 5)?,
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
    let mut db = database(&newest(&home, "state_")?)?;
    // Keep the lightweight scan and deferred title reads on one read snapshot.
    let db = db.transaction().map_err(|_| "无法读取 Codex 数据库")?;
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
    let mut sessions: Vec<_> = all
        .into_iter()
        .take(c.limit)
        .map(|r| {
            let mut s = r.session;
            s.project_name = if c.include_project {
                bounded(&s.project_name, 96)
            } else {
                String::new()
            };
            s.model = bounded(&s.model, 64);
            s
        })
        .collect();
    if c.include_title {
        session_titles(&db, &mut sessions)?;
    }
    for session in &mut sessions {
        session.session_id = bounded(&session.session_id, 128);
    }
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
// Only allowlisted categories and numeric codes cross into status/UI. Never retain
// RPC messages, stderr, URLs or credentials in diagnostics.
#[derive(Clone, Debug, serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WeeklyError {
    pub stage: &'static str,
    pub code: &'static str,
    pub rpc_code: Option<i64>,
    pub http_status: Option<u16>,
}
impl WeeklyError {
    pub fn new(stage: &'static str, code: &'static str) -> Self {
        Self {
            stage,
            code,
            rpc_code: None,
            http_status: None,
        }
    }
    fn transient(&self) -> bool {
        matches!(self.code, "timeout" | "transport" | "closed" | "network")
            || (self.stage == "request" && matches!(self.code, "server" | "rpc"))
    }
}
impl std::fmt::Display for WeeklyError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(match (self.stage, self.code) {
            (_, "start") => "无法启动 Codex，请检查程序位置",
            (_, "authentication") => "Codex 登录已失效或尚未登录，请在 Codex 中登录",
            (_, "forbidden") => "Codex 拒绝访问周限，请检查账户权限或网络访问限制",
            (_, "rate_limited") => "Codex 请求过于频繁，将稍后自动重试",
            (_, "unsupported") => "当前 Codex 不支持读取周限，请检查版本或登录方式",
            ("initialize", "timeout") => "Codex 初始化超时，将自动重试",
            ("initialize", _) => "Codex 初始化失败，将稍后重试；持续失败请检查版本",
            (_, "timeout") => "读取 Codex 周限超时，将自动重试",
            (_, "transport" | "closed") => "与 Codex 的连接中断，将自动重试",
            (_, "server") => "Codex 服务暂不可用，将自动重试",
            (_, "network") => "无法连接 Codex 服务，请检查网络；将自动重试",
            (_, "unavailable") => "Codex 未返回周限，将稍后自动重试",
            (_, "invalid_data") => "Codex 周限数据无效或已重置，将稍后自动重试",
            _ => "读取 Codex 周限失败，将自动重试",
        })
    }
}
impl std::error::Error for WeeklyError {}

fn weekly_rpc_error(stage: &'static str, error: &Value) -> WeeklyError {
    let rpc_code = error["code"].as_i64();
    // Older Codex versions only expose HTTP status in the message. Read it for
    // classification, but never copy any part of the message into diagnostics.
    let message = error["message"].as_str().unwrap_or("").to_ascii_lowercase();
    let http_status = ["httpStatus", "statusCode", "status"]
        .iter()
        .find_map(|key| error["data"][key].as_u64())
        .filter(|n| (400..600).contains(n))
        .map(|n| n as u16)
        .or_else(|| {
            [401u16, 403, 429, 500, 502, 503, 504]
                .into_iter()
                .find(|n| {
                    [
                        format!("http {n}"),
                        format!("status {n}"),
                        format!("status code: {n}"),
                    ]
                    .iter()
                    .any(|pattern| message.contains(pattern))
                })
        });
    let code = match http_status {
        Some(401) => "authentication",
        Some(403) => "forbidden",
        Some(429) => "rate_limited",
        Some(500..=599) => "server",
        _ if rpc_code == Some(-32601) => "unsupported",
        _ if [
            "not logged in",
            "authentication required",
            "requires chatgpt authentication",
            "refresh token has expired",
            "refresh token has already been used",
        ]
        .iter()
        .any(|s| message.contains(s)) =>
        {
            "authentication"
        }
        _ if message.contains("not supported") => "unsupported",
        _ if message.contains("timed out") || message.contains("timeout") => "timeout",
        _ if [
            "error sending request",
            "failed to connect",
            "dns error",
            "connection refused",
            "connection reset",
        ]
        .iter()
        .any(|s| message.contains(s)) =>
        {
            "network"
        }
        _ => "rpc",
    };
    WeeklyError {
        stage,
        code,
        rpc_code,
        http_status,
    }
}

#[derive(Default)]
pub struct WeeklyRetry {
    failures: usize,
}
impl WeeklyRetry {
    pub fn delay(&mut self, error: Option<&WeeklyError>) -> Duration {
        match error {
            None => {
                self.failures = 0;
                Duration::from_secs(60)
            }
            Some(error) if error.transient() => {
                let seconds = [5, 15, 30, 60][self.failures.min(3)];
                self.failures = (self.failures + 1).min(3);
                Duration::from_secs(seconds)
            }
            Some(_) => {
                self.failures = 0;
                Duration::from_secs(60)
            }
        }
    }
}

async fn weekly_response<R: tokio::io::AsyncBufRead + Unpin>(
    output: &mut R,
    expected_id: i64,
    stage: &'static str,
) -> Result<Value, WeeklyError> {
    loop {
        let mut bytes = Vec::new();
        let n = (&mut *output)
            .take(1048577)
            .read_until(b'\n', &mut bytes)
            .await
            .map_err(|_| WeeklyError::new(stage, "transport"))?;
        if n == 0 {
            return Err(WeeklyError::new(stage, "closed"));
        }
        if n > 1048576 {
            return Err(WeeklyError::new(stage, "invalid_data"));
        }
        let mut v: Value =
            serde_json::from_slice(&bytes).map_err(|_| WeeklyError::new(stage, "invalid_data"))?;
        if v["id"].as_i64() != Some(expected_id) {
            continue;
        }
        if !v["error"].is_null() {
            return Err(weekly_rpc_error(stage, &v["error"]));
        }
        if v.get("result").is_none_or(Value::is_null) {
            return Err(WeeklyError::new(stage, "invalid_data"));
        }
        return Ok(v["result"].take());
    }
}

async fn weekly_exchange<W, R>(
    input: &mut W,
    output: &mut R,
    now: i64,
    initialize_timeout: Duration,
    request_timeout: Duration,
) -> Result<CodexWeeklyLimit, WeeklyError>
where
    W: tokio::io::AsyncWrite + Unpin,
    R: tokio::io::AsyncBufRead + Unpin,
{
    tokio::time::timeout(initialize_timeout, async {
        input.write_all(b"{\"id\":1,\"method\":\"initialize\",\"params\":{\"clientInfo\":{\"name\":\"orbit\",\"title\":\"Orbit\",\"version\":\"0.1.0\"}}}\n").await
            .map_err(|_| WeeklyError::new("initialize", "transport"))?;
        weekly_response(output, 1, "initialize").await
    }).await.map_err(|_| WeeklyError::new("initialize", "timeout"))??;
    let result = tokio::time::timeout(request_timeout, async {
        input.write_all(b"{\"method\":\"initialized\"}\n{\"id\":2,\"method\":\"account/rateLimits/read\"}\n").await
            .map_err(|_| WeeklyError::new("request", "transport"))?;
        weekly_response(output, 2, "request").await
    }).await.map_err(|_| WeeklyError::new("request", "timeout"))??;
    parse_weekly(&result, now).map_err(|message| {
        WeeklyError::new(
            "parse",
            if message == "Codex 周限不可用" {
                "unavailable"
            } else {
                "invalid_data"
            },
        )
    })
}

pub async fn weekly(c: &Codex, now: i64) -> Result<CodexWeeklyLimit, WeeklyError> {
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
        .map_err(|_| WeeklyError::new("spawn", "start"))?;
    let result = async {
        let mut input = child
            .stdin
            .take()
            .ok_or_else(|| WeeklyError::new("initialize", "transport"))?;
        let mut output = BufReader::new(
            child
                .stdout
                .take()
                .ok_or_else(|| WeeklyError::new("initialize", "transport"))?,
        );
        weekly_exchange(
            &mut input,
            &mut output,
            now,
            Duration::from_secs(5),
            Duration::from_secs(15),
        )
        .await
    }
    .await;
    let _ = child.kill().await;
    let _ = child.wait().await;
    result
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn weekly_rpc_errors_are_classified_without_retaining_private_text() {
        let cases = [
            (
                serde_json::json!({"code":-32000,"message":"HTTP 401: token=private-secret https://private.invalid"}),
                "authentication",
                Some(401),
            ),
            (
                serde_json::json!({"code":-32000,"message":"HTTP 403 Forbidden"}),
                "forbidden",
                Some(403),
            ),
            (
                serde_json::json!({"code":-32000,"message":"private-secret", "data":{"statusCode":429}}),
                "rate_limited",
                Some(429),
            ),
            (
                serde_json::json!({"code":-32000,"message":"unexpected status 503 private-secret"}),
                "server",
                Some(503),
            ),
            (
                serde_json::json!({"code":-32601,"message":"private-secret"}),
                "unsupported",
                None,
            ),
            (
                serde_json::json!({"code":-32000,"message":"Not logged in private-secret"}),
                "authentication",
                None,
            ),
            (
                serde_json::json!({"code":-32000,"message":"request timed out private-secret"}),
                "timeout",
                None,
            ),
            (
                serde_json::json!({"code":-32000,"message":"error sending request for url (https://private.invalid)"}),
                "network",
                None,
            ),
            (
                serde_json::json!({"code":-32000,"message":"unknown failure private-secret"}),
                "rpc",
                None,
            ),
        ];
        for (value, code, http) in cases {
            let error = weekly_rpc_error("request", &value);
            assert_eq!(error.code, code);
            assert_eq!(error.http_status, http);
            assert_eq!(error.rpc_code, value["code"].as_i64());
            let safe = format!(
                "{} {:?} {}",
                error,
                error,
                serde_json::to_string(&error).unwrap()
            );
            assert!(!safe.contains("private"));
            if code != "authentication" && code != "unsupported" {
                assert!(!error.to_string().contains("登录"));
            }
        }
    }

    #[test]
    fn weekly_retries_back_off_and_reset_after_recovery() {
        let mut retry = WeeklyRetry::default();
        let failure = WeeklyError::new("request", "timeout");
        for seconds in [5, 15, 30, 60, 60, 60] {
            assert_eq!(retry.delay(Some(&failure)), Duration::from_secs(seconds));
        }
        assert_eq!(retry.delay(None), Duration::from_secs(60));
        assert_eq!(retry.delay(Some(&failure)), Duration::from_secs(5));
        for code in [
            "authentication",
            "forbidden",
            "unsupported",
            "rate_limited",
            "invalid_data",
            "start",
        ] {
            assert_eq!(
                retry.delay(Some(&WeeklyError::new("request", code))),
                Duration::from_secs(60)
            );
        }
        assert_eq!(retry.delay(Some(&failure)), Duration::from_secs(5));
        assert_eq!(
            retry.delay(Some(&WeeklyError::new("initialize", "timeout"))),
            Duration::from_secs(15)
        );
    }

    #[tokio::test]
    async fn weekly_exchange_obeys_handshake_and_ignores_notifications() {
        let (client, server) = tokio::io::duplex(4096);
        let (mut read, mut write) = tokio::io::split(client);
        let server = tokio::spawn(async move {
            let (read, mut write) = tokio::io::split(server);
            let mut read = BufReader::new(read).lines();
            let v: Value = serde_json::from_str(&read.next_line().await.unwrap().unwrap()).unwrap();
            assert_eq!(v["method"], "initialize");
            write
                .write_all(b"{\"method\":\"notice\"}\n{\"id\":1,\"result\":{}}\n")
                .await
                .unwrap();
            let v: Value = serde_json::from_str(&read.next_line().await.unwrap().unwrap()).unwrap();
            assert_eq!(v["method"], "initialized");
            let v: Value = serde_json::from_str(&read.next_line().await.unwrap().unwrap()).unwrap();
            assert_eq!(v["method"], "account/rateLimits/read");
            write.write_all(b"{\"id\":99,\"error\":{}}\n{\"id\":2,\"result\":{\"rateLimits\":{\"primary\":{\"windowDurationMins\":10080,\"usedPercent\":25,\"resetsAt\":1000}}}}\n").await.unwrap();
        });
        let result = weekly_exchange(
            &mut write,
            &mut BufReader::new(&mut read),
            100,
            Duration::from_secs(1),
            Duration::from_secs(1),
        )
        .await
        .unwrap();
        assert_eq!(result.remaining_percent, 75.);
        assert_eq!(result.observed_at.unwrap().seconds, 100);
        assert_eq!(result.fresh_until.unwrap().seconds, 280);
        server.await.unwrap();
    }

    #[tokio::test]
    async fn weekly_exchange_distinguishes_phase_timeouts() {
        for (reply, stage) in [
            ("", "initialize"),
            ("{\"id\":1,\"result\":{}}\n", "request"),
        ] {
            let (read, mut peer) = tokio::io::duplex(4096);
            peer.write_all(reply.as_bytes()).await.unwrap();
            let error = weekly_exchange(
                &mut tokio::io::sink(),
                &mut BufReader::new(read),
                100,
                Duration::from_millis(10),
                Duration::from_millis(10),
            )
            .await
            .unwrap_err();
            assert_eq!(error.stage, stage);
            assert_eq!(error.code, "timeout");
        }
    }

    #[tokio::test]
    async fn weekly_exchange_rejects_bad_responses_without_claiming_auth_failure() {
        for (reply, stage, code) in [
            ("", "initialize", "closed"),
            ("not-json\n", "initialize", "invalid_data"),
            ("{\"id\":1,\"result\":{}}\n", "request", "closed"),
            (
                "{\"id\":1,\"error\":{\"code\":-32601}}\n",
                "initialize",
                "unsupported",
            ),
            (
                "{\"id\":1,\"result\":{}}\n{\"id\":2,\"result\":{}}\n",
                "parse",
                "unavailable",
            ),
            (
                "{\"id\":1,\"result\":{}}\n{\"id\":2,\"result\":null}\n",
                "request",
                "invalid_data",
            ),
        ] {
            let mut output = BufReader::new(reply.as_bytes());
            let error = weekly_exchange(
                &mut tokio::io::sink(),
                &mut output,
                100,
                Duration::from_secs(1),
                Duration::from_secs(1),
            )
            .await
            .unwrap_err();
            assert_eq!((error.stage, error.code), (stage, code));
        }
        let bytes = vec![b'x'; 1048577];
        let error = weekly_response(&mut BufReader::new(bytes.as_slice()), 1, "initialize")
            .await
            .unwrap_err();
        assert_eq!(error.code, "invalid_data");
    }

    #[tokio::test]
    async fn weekly_spawn_failure_is_actionable() {
        let config = Codex {
            binary: "/nonexistent/orbit-test-codex".into(),
            ..Codex::default()
        };
        let error = weekly(&config, 100).await.unwrap_err();
        assert_eq!((error.stage, error.code), ("spawn", "start"));
    }
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

    #[test]
    fn visible_titles_preserve_fallbacks_after_filtering_and_limit() {
        let dir = tempfile::tempdir().unwrap();
        let db = Connection::open(dir.path().join("state_1.sqlite")).unwrap();
        db.execute_batch("CREATE TABLE threads(id TEXT PRIMARY KEY,name TEXT,title TEXT,source TEXT,cwd TEXT,model TEXT,updated_at_ms INTEGER,first_user_message TEXT,archived INTEGER,rollout_path TEXT);
            INSERT INTO threads VALUES
            ('hidden','Hidden','','{\"subagent\":{}}','/work','gpt',600000,'',0,NULL),
            ('a','  Named  session  ','Ignored title','cli','/work','gpt',500000,'Ignored message',0,NULL),
            ('b',' \n ','  Title  fallback  ','cli','/work','gpt',400000,'Ignored message',0,NULL),
            ('c',NULL,' \t ','cli','/work','gpt',300000,'  Message  fallback  ',0,NULL),
            ('d',NULL,NULL,'cli','/work','gpt',200000,NULL,0,NULL);").unwrap();
        Connection::open(dir.path().join("thread_history_1.sqlite")).unwrap()
            .execute_batch("CREATE TABLE thread_turns(thread_id TEXT,turn_id TEXT,status TEXT,started_at INTEGER,completed_at INTEGER);
                INSERT INTO thread_turns VALUES('d','turn','in_progress',200,NULL);").unwrap();
        let mut c = Codex {
            home: dir.path().display().to_string(),
            include_title: true,
            limit: 2,
            ..Codex::default()
        };
        let result = collect(&c, 201).unwrap();
        assert_eq!(result.total_count, 4);
        assert_eq!(result.running_count, 1); // Counts include sessions outside the display limit.
        assert_eq!(
            result
                .sessions
                .iter()
                .map(|s| s.display_name.as_str())
                .collect::<Vec<_>>(),
            ["Named session", "Title fallback"]
        );
        c.limit = 4;
        let result = collect(&c, 201).unwrap();
        assert_eq!(result.sessions[2].display_name, "Message fallback");
        assert_eq!(result.sessions[3].display_name, "(无标题)");
        db.execute_batch("ALTER TABLE threads DROP COLUMN name;")
            .unwrap();
        let result = collect(&c, 201).unwrap();
        assert_eq!(result.sessions[0].display_name, "Ignored title");
        c.include_title = false;
        assert!(collect(&c, 201)
            .unwrap()
            .sessions
            .iter()
            .all(|s| s.display_name.is_empty()));
    }

    #[test]
    #[ignore = "explicit local read-only performance check"]
    fn live_collection_benchmark() {
        let home = std::env::var("ORBIT_CODEX_BENCH_HOME").expect("set ORBIT_CODEX_BENCH_HOME");
        let c = Codex {
            home,
            include_title: true,
            include_project: true,
            ..Codex::default()
        };
        for _ in 0..3 {
            let start = std::time::Instant::now();
            let result = collect(&c, chrono::Utc::now().timestamp()).unwrap();
            eprintln!(
                "collection_ms={} total={} displayed={} running={}",
                start.elapsed().as_millis(),
                result.total_count,
                result.sessions.len(),
                result.running_count
            );
        }
    }
}
