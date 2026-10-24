use super::protocol::{digest, encode, Job, ResultMessage};
use rusqlite::{params, Connection, OptionalExtension};
use std::{path::Path, sync::Mutex};

#[derive(Debug, thiserror::Error)]
pub enum StoreError {
    #[error("database error")]
    Database(#[from] rusqlite::Error),
    #[error("job id reused with changed content")]
    Conflict,
    #[error("durable queue full; job not acknowledged")]
    Full,
    #[error("invalid result")]
    Result,
    #[error("protocol error")]
    Protocol,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct Counts {
    pub queued: u32,
    pub processing: u32,
    pub outbox: u32,
}

pub struct Store {
    db: Mutex<Connection>,
    capacity: u32,
}

impl Store {
    pub fn open(path: &Path, capacity: u32) -> Result<Self, StoreError> {
        let db = Connection::open(path)?;
        db.pragma_update(None, "journal_mode", "WAL")?;
        db.pragma_update(None, "synchronous", "FULL")?;
        db.execute_batch("CREATE TABLE IF NOT EXISTS jobs (id TEXT PRIMARY KEY, digest TEXT NOT NULL, payload BLOB, status TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, result BLOB); CREATE INDEX IF NOT EXISTS jobs_status ON jobs(status);")?;
        db.execute(
            "UPDATE jobs SET status='queued' WHERE status='processing'",
            [],
        )?;
        Ok(Self {
            db: Mutex::new(db),
            capacity: capacity.max(1),
        })
    }

    pub fn accept(&self, job: &Job) -> Result<bool, StoreError> {
        let payload = serde_json::to_vec(job).map_err(|_| StoreError::Protocol)?;
        let digest = digest(job).map_err(|_| StoreError::Protocol)?;
        let db = self.db.lock().unwrap();
        let tx = db.unchecked_transaction()?;
        if let Some(existing) = tx
            .query_row("SELECT digest FROM jobs WHERE id=?", [&job.job_id], |r| {
                r.get::<_, String>(0)
            })
            .optional()?
        {
            if existing != digest {
                return Err(StoreError::Conflict);
            }
            tx.commit()?;
            return Ok(false);
        }
        let count: u32 = tx.query_row(
            "SELECT count(*) FROM jobs WHERE status != 'done'",
            [],
            |r| r.get(0),
        )?;
        if count >= self.capacity {
            return Err(StoreError::Full);
        }
        tx.execute(
            "INSERT INTO jobs(id,digest,payload,status) VALUES(?,?,?,'queued')",
            params![job.job_id, digest, payload],
        )?;
        tx.commit()?;
        Ok(true)
    }

    pub fn claim(&self) -> Result<Option<(Job, u32)>, StoreError> {
        let db = self.db.lock().unwrap();
        let tx = db.unchecked_transaction()?;
        let row = tx
            .query_row(
                "SELECT id,payload,attempts FROM jobs WHERE status='queued' ORDER BY rowid LIMIT 1",
                [],
                |r| {
                    let id: String = r.get(0)?;
                    let payload: Vec<u8> = r.get(1)?;
                    let attempts: u32 = r.get(2)?;
                    Ok((id, payload, attempts))
                },
            )
            .optional()?;
        let Some((id, payload, attempts)) = row else {
            tx.commit()?;
            return Ok(None);
        };
        tx.execute(
            "UPDATE jobs SET status='processing',attempts=attempts+1 WHERE id=?",
            [&id],
        )?;
        tx.commit()?;
        let job = serde_json::from_slice(&payload).map_err(|_| StoreError::Protocol)?;
        Ok(Some((job, attempts + 1)))
    }

    pub fn retry(&self, job_id: &str) -> Result<(), StoreError> {
        self.db.lock().unwrap().execute(
            "UPDATE jobs SET status='queued' WHERE id=? AND status='processing'",
            [job_id],
        )?;
        Ok(())
    }

    pub fn finish(&self, job_id: &str, result: &ResultMessage) -> Result<Vec<u8>, StoreError> {
        let payload = encode(result).map_err(|_| StoreError::Protocol)?;
        let db = self.db.lock().unwrap();
        let tx = db.unchecked_transaction()?;
        let changed = tx.execute("UPDATE jobs SET status='outbox',result=?,payload=NULL WHERE id=? AND status='processing'", params![&payload, job_id])?;
        if changed != 1 {
            return Err(StoreError::Result);
        }
        tx.commit()?;
        Ok(payload)
    }

    pub fn outbox(&self) -> Result<Vec<(String, Vec<u8>)>, StoreError> {
        let db = self.db.lock().unwrap();
        let mut stmt =
            db.prepare("SELECT id,result FROM jobs WHERE status='outbox' ORDER BY rowid LIMIT 20")?;
        let rows = stmt
            .query_map([], |r| Ok((r.get(0)?, r.get(1)?)))?
            .collect::<Result<Vec<_>, _>>()?;
        Ok(rows)
    }

    pub fn acknowledge(&self, job_id: &str) -> Result<(), StoreError> {
        self.db.lock().unwrap().execute(
            "UPDATE jobs SET status='done',result=NULL WHERE id=? AND status='outbox'",
            [job_id],
        )?;
        Ok(())
    }

    pub fn counts(&self) -> Result<Counts, StoreError> {
        let db = self.db.lock().unwrap();
        let mut counts = Counts::default();
        let mut stmt = db.prepare("SELECT status,count(*) FROM jobs WHERE status IN ('queued','processing','outbox') GROUP BY status")?;
        for row in stmt.query_map([], |r| Ok((r.get::<_, String>(0)?, r.get::<_, u32>(1)?)))? {
            let (status, count) = row?;
            match status.as_str() {
                "queued" => counts.queued = count,
                "processing" => counts.processing = count,
                "outbox" => counts.outbox = count,
                _ => {}
            }
        }
        Ok(counts)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::inference::protocol::{Job, Segment};
    use tempfile::tempdir;

    fn job(id: &str) -> Job {
        Job {
            schema_version: 1,
            job_id: id.into(),
            segment: Segment {
                schema_version: 1,
                segment_id: id.into(),
                session_id: "00000000-0000-4000-8000-000000000002".into(),
                node_id: "android-1".into(),
                sequence: 0,
                started_at: "2026-10-24T00:00:00Z".into(),
                duration_ms: 20,
                speech_ms: 20,
                sample_rate: 16_000,
                channels: 1,
                codec: "opus".into(),
                frame_ms: 20,
                packets: vec!["AQ==".into()],
            },
        }
    }
    fn result(id: &str) -> ResultMessage {
        ResultMessage {
            schema_version: 1,
            job_id: id.into(),
            segment_id: id.into(),
            node_id: "android-1".into(),
            agent_id: "sdt-asr".into(),
            status: "completed".into(),
            text: "hello".into(),
            language: "en".into(),
            segments: vec![],
            model: "mock".into(),
            device: "cpu".into(),
            processing_ms: 1,
            completed_at: "2026-10-24T00:00:00Z".into(),
            error_code: String::new(),
            error_message: String::new(),
        }
    }
    #[test]
    fn durable_lifecycle_preserves_tombstone_and_outbox() {
        let dir = tempdir().unwrap();
        let path = dir.path().join("queue.sqlite");
        {
            let store = Store::open(&path, 1).unwrap();
            let first = job("00000000-0000-4000-8000-000000000001");
            assert!(store.accept(&first).unwrap());
            assert!(!store.accept(&first).unwrap());
            let mut changed = first.clone();
            changed.segment.sequence = 1;
            assert!(matches!(store.accept(&changed), Err(StoreError::Conflict)));
            let (claimed, attempts) = store.claim().unwrap().unwrap();
            assert_eq!(claimed.job_id, first.job_id);
            assert_eq!(attempts, 1);
            store.finish(&first.job_id, &result(&first.job_id)).unwrap();
            assert_eq!(store.counts().unwrap().outbox, 1);
        }
        let store = Store::open(&path, 1).unwrap();
        assert_eq!(store.outbox().unwrap().len(), 1);
        store
            .acknowledge("00000000-0000-4000-8000-000000000001")
            .unwrap();
        assert_eq!(store.counts().unwrap(), Counts::default());
        assert!(!store
            .accept(&job("00000000-0000-4000-8000-000000000001"))
            .unwrap());
    }
}
