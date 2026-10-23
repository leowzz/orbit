"""Durable bounded jobs and application-acknowledged result outbox."""
import hashlib
import json
import sqlite3
import threading
from .protocol import encode


class Store:
    def __init__(self, path, capacity=1000):
        self.lock = threading.RLock()
        self.capacity = capacity
        self.db = sqlite3.connect(path, check_same_thread=False)
        self.db.execute("PRAGMA journal_mode=WAL")
        self.db.execute("PRAGMA synchronous=FULL")
        self.db.execute("CREATE TABLE IF NOT EXISTS jobs (id TEXT PRIMARY KEY, digest TEXT NOT NULL, payload BLOB, status TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, result BLOB)")
        self.db.execute("CREATE INDEX IF NOT EXISTS jobs_status ON jobs(status)")
        self.db.execute("UPDATE jobs SET status='queued' WHERE status='processing'")
        self.db.commit()

    def accept(self, job):
        payload = json.dumps(job, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()
        digest = hashlib.sha256(payload).hexdigest()
        with self.lock, self.db:
            row = self.db.execute("SELECT digest FROM jobs WHERE id=?", (job['job_id'],)).fetchone()
            if row:
                if row[0] != digest:
                    raise ValueError("job ID reused with changed content")
                return False
            count = self.db.execute("SELECT count(*) FROM jobs WHERE status != 'done'").fetchone()[0]
            if count >= self.capacity:
                raise OverflowError("durable queue full; job not acknowledged")
            self.db.execute("INSERT INTO jobs(id,digest,payload,status) VALUES(?,?,?,'queued')", (job['job_id'],digest,payload))
            return True

    def claim(self):
        with self.lock, self.db:
            row = self.db.execute("SELECT id,payload,attempts FROM jobs WHERE status='queued' ORDER BY rowid LIMIT 1").fetchone()
            if row:
                self.db.execute("UPDATE jobs SET status='processing',attempts=attempts+1 WHERE id=?", (row[0],))
                return json.loads(row[1]), row[2]+1

    def retry(self, job_id):
        with self.lock, self.db:
            self.db.execute("UPDATE jobs SET status='queued' WHERE id=?", (job_id,))

    def finish(self, job_id, result):
        payload = encode(result)
        with self.lock, self.db:
            self.db.execute("UPDATE jobs SET status='outbox',result=?,payload=NULL WHERE id=?", (payload,job_id))

    def outbox(self):
        with self.lock:
            return self.db.execute("SELECT result FROM jobs WHERE status='outbox' ORDER BY rowid LIMIT 20").fetchall()

    def acknowledge(self, job_id):
        with self.lock, self.db:
            # Keep digest tombstones indefinitely: Core may resend old jobs.
            self.db.execute("UPDATE jobs SET status='done',result=NULL WHERE id=? AND status='outbox'", (job_id,))


    def counts(self):
        with self.lock:
            counts = dict(queued=0,processing=0,outbox=0)
            for status,count in self.db.execute("SELECT status,count(*) FROM jobs WHERE status IN ('queued','processing','outbox') GROUP BY status"):
                if status in counts:
                    counts[status] = count
            return counts
