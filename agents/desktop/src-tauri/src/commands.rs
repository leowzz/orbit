use crate::{
    codex::ts,
    wire::{self, Command, CommandResult, Metadata},
};
use prost::Message;
use sha2::{Digest, Sha256};
use std::collections::VecDeque;

pub fn metadata(id: &str, revision: u64, now: i64, ttl: Option<i64>) -> Metadata {
    Metadata {
        message_id: uuid::Uuid::new_v4().to_string(),
        producer_id: id.into(),
        revision,
        produced_at: ts(now),
        expires_at: ttl.and_then(|n| ts(now + n)),
    }
}
pub fn thread_url(id: &str) -> Result<String, String> {
    let parsed = uuid::Uuid::parse_str(id).map_err(|_| "会话编号无效")?;
    if parsed.hyphenated().to_string() != id {
        return Err("会话编号无效".into());
    }
    Ok(format!("codex://threads/{id}"))
}
pub fn validate(
    c: &Command,
    id: &str,
    enabled: bool,
    now: i64,
) -> Result<String, (i32, &'static str)> {
    validate_at(c, id, enabled, now as i128 * 1_000_000_000)
}
fn validate_at(
    c: &Command,
    id: &str,
    enabled: bool,
    now_nanos: i128,
) -> Result<String, (i32, &'static str)> {
    let bad = || (3, "invalid_command");
    let m = c.metadata.as_ref().ok_or_else(bad)?;
    let r = c.intent_ref.as_ref().ok_or_else(bad)?;
    if m.message_id.is_empty()
        || m.producer_id.is_empty()
        || m.revision == 0
        || c.command_id.is_empty()
        || c.command_id.len() > 128
        || c.target_agent_id != id
        || r.intent_id.is_empty()
        || r.requester_id.is_empty()
        || r.requester_kind != 1
    {
        return Err(bad());
    }
    let produced = m.produced_at.as_ref().ok_or((3, "invalid_command_time"))?;
    let expiry = m.expires_at.as_ref().ok_or((3, "invalid_command_time"))?;
    let valid = |t: &prost_types::Timestamp| {
        t.seconds >= -62135596800
            && t.seconds <= 253402300799
            && (0..1_000_000_000).contains(&t.nanos)
    };
    let nanos = |t: &prost_types::Timestamp| t.seconds as i128 * 1_000_000_000 + t.nanos as i128;
    if !valid(produced)
        || !valid(expiry)
        || nanos(expiry) <= nanos(produced)
        || nanos(expiry) - nanos(produced) > 30_000_000_000
        || nanos(produced) > now_nanos + 10_000_000_000
    {
        return Err((3, "invalid_command_time"));
    }
    if nanos(expiry) <= now_nanos {
        return Err((4, "command_expired"));
    }
    let Some(wire::command::Action::OpenCodexSession(action)) = &c.action else {
        return Err((3, "unsupported_action"));
    };
    if !enabled {
        return Err((3, "unsupported_action"));
    }
    thread_url(&action.session_id).map_err(|_| (3, "invalid_session_id"))
}
#[derive(Default)]
pub struct Executor {
    cache: VecDeque<(String, Vec<u8>, CommandResult)>,
    revision: u64,
}
impl Executor {
    pub async fn execute(
        &mut self,
        c: Command,
        id: &str,
        enabled: bool,
        now: i64,
    ) -> CommandResult {
        self.execute_with(c, id, enabled, now, |url| async move {
            for command in open::commands(url) {
                let mut command = tokio::process::Command::from(command);
                command
                    .kill_on_drop(true)
                    .stdout(std::process::Stdio::null())
                    .stderr(std::process::Stdio::null());
                #[cfg(windows)]
                command.creation_flags(0x08000000);
                if command.status().await.is_ok_and(|s| s.success()) {
                    return Ok(());
                }
            }
            Err(())
        })
        .await
    }
    async fn execute_with<F, Fut>(
        &mut self,
        c: Command,
        id: &str,
        enabled: bool,
        now: i64,
        open: F,
    ) -> CommandResult
    where
        F: FnOnce(String) -> Fut,
        Fut: std::future::Future<Output = Result<(), ()>>,
    {
        let fingerprint = Sha256::digest(c.encode_to_vec()).to_vec();
        if let Some((_, hash, result)) = self.cache.iter().find(|(cid, _, _)| cid == &c.command_id)
        {
            if hash == &fingerprint {
                return result.clone();
            }
            return self.result(&c, id, now / 1000, 3, "command_id_conflict");
        }
        let (status, error) = match validate_at(&c, id, enabled, now as i128 * 1_000_000) {
            Ok(url) => {
                match tokio::time::timeout(std::time::Duration::from_secs(5), open(url)).await {
                    Ok(Ok(())) => (1, ""),
                    _ => (2, "open_failed"),
                }
            }
            Err(e) => e,
        };
        let result = self.result(&c, id, now / 1000, status, error);
        if !c.command_id.is_empty() {
            if self.cache.len() == 256 {
                self.cache.pop_front();
            }
            self.cache
                .push_back((c.command_id, fingerprint, result.clone()));
        }
        result
    }
    fn result(
        &mut self,
        c: &Command,
        id: &str,
        now: i64,
        status: i32,
        error: &str,
    ) -> CommandResult {
        self.revision += 1;
        CommandResult {
            metadata: Some(metadata(id, self.revision, now, None)),
            command_id: c.command_id.clone(),
            intent_ref: c.intent_ref.clone(),
            status,
            error_code: error.into(),
            safe_message: if status == 1 {
                "Codex session opened"
            } else {
                "Codex session was not opened"
            }
            .into(),
        }
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn command() -> Command {
        Command {
            metadata: Some(metadata("core", 1, 100, Some(20))),
            command_id: "command".into(),
            target_agent_id: "agent".into(),
            intent_ref: Some(wire::IntentRef {
                intent_id: "intent".into(),
                requester_kind: 1,
                requester_id: "node".into(),
            }),
            intent_produced_at: ts(100),
            action: Some(wire::command::Action::OpenCodexSession(
                wire::OpenCodexSession {
                    session_id: "01a06712-3498-74b3-b787-532466bf8eb8".into(),
                },
            )),
        }
    }
    #[test]
    fn rejects_identity_ttl_and_disabled() {
        let c = command();
        assert!(validate(&c, "agent", true, 101).is_ok());
        assert!(validate(&c, "other", true, 101).is_err());
        assert_eq!(
            validate(&c, "agent", true, 121).unwrap_err().1,
            "command_expired"
        );
        assert!(validate(&c, "agent", false, 101).is_err());
        assert!(thread_url("--help;anything").is_err());
    }
    #[test]
    fn expiration_is_checked_at_subsecond_precision() {
        let mut c = command();
        c.metadata.as_mut().unwrap().expires_at = Some(prost_types::Timestamp {
            seconds: 101,
            nanos: 100_000_000,
        });
        assert_eq!(
            validate_at(&c, "agent", true, 101_900_000_000)
                .unwrap_err()
                .1,
            "command_expired"
        );
    }
    #[tokio::test]
    async fn duplicate_executes_once_and_conflict_rejected() {
        let mut e = Executor::default();
        let c = command();
        let a = e
            .execute_with(c.clone(), "agent", true, 101000, |_| async { Ok(()) })
            .await;
        let b = e
            .execute_with(c.clone(), "agent", true, 102000, |_| async {
                panic!("duplicate executed")
            })
            .await;
        assert_eq!(a, b);
        let mut different = c;
        different.target_agent_id = "other".into();
        let r = e
            .execute_with(different, "agent", true, 103000, |_| async {
                panic!("conflict executed")
            })
            .await;
        assert_eq!(r.error_code, "command_id_conflict");
    }
}
