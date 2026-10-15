use bytes::BytesMut;
use orbit_desktop_agent::{
    commands::metadata,
    config::{Config, Secrets},
    runtime::{run, Status},
    wire,
};
use prost::Message;
use rumqttc::v5::mqttbytes::{v5::*, Error, QoS};
use std::{
    sync::{Arc, Mutex},
    time::Duration,
};
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt},
    net::{TcpListener, TcpStream},
};
use tokio_util::sync::CancellationToken;
struct Peer {
    socket: TcpStream,
    bytes: BytesMut,
}
impl Peer {
    async fn read(&mut self) -> Packet {
        loop {
            match Packet::read(&mut self.bytes, Some(65536)) {
                Ok(p) => return p,
                Err(Error::InsufficientBytes(_)) => {
                    assert_ne!(self.socket.read_buf(&mut self.bytes).await.unwrap(), 0)
                }
                Err(e) => panic!("{e:?}"),
            }
        }
    }
    async fn send(&mut self, p: Packet) {
        let mut bytes = BytesMut::new();
        p.write(&mut bytes).unwrap();
        self.socket.write_all(&bytes).await.unwrap()
    }
    async fn connect(listener: &TcpListener) -> Self {
        let (socket, _) = listener.accept().await.unwrap();
        let mut peer = Self {
            socket,
            bytes: BytesMut::new(),
        };
        assert!(matches!(peer.read().await, Packet::Connect(..)));
        peer.send(Packet::ConnAck(ConnAck {
            session_present: false,
            code: ConnectReturnCode::Success,
            properties: None,
        }))
        .await;
        peer
    }
    async fn publication(&mut self, suffix: &str) -> Publish {
        loop {
            match self.read().await {
                Packet::Subscribe(s) => {
                    assert!(s.filters[0].preserve_retain);
                    self.send(Packet::SubAck(SubAck {
                        pkid: s.pkid,
                        return_codes: vec![SubscribeReasonCode::Success(QoS::AtLeastOnce)],
                        properties: None,
                    }))
                    .await
                }
                Packet::Publish(p) => {
                    assert_eq!(p.qos, QoS::AtLeastOnce);
                    assert_eq!(
                        p.properties
                            .as_ref()
                            .and_then(|p| p.content_type.as_deref()),
                        Some("application/protobuf")
                    );
                    self.send(Packet::PubAck(PubAck::new(p.pkid, None))).await;
                    if p.topic.ends_with(suffix.as_bytes()) {
                        return p;
                    }
                }
                Packet::PingReq(_) => self.send(Packet::PingResp(PingResp)).await,
                _ => {}
            }
        }
    }
}
#[tokio::test]
async fn reconnect_announces_epoch_then_observation_and_takeover_stops() {
    tokio::time::timeout(Duration::from_secs(20), scenario())
        .await
        .expect("MQTT runtime timed out");
}
async fn scenario() {
    let dir = tempfile::tempdir().unwrap();
    let db = rusqlite::Connection::open(dir.path().join("state_1.sqlite")).unwrap();
    db.execute_batch("CREATE TABLE threads(id TEXT,title TEXT,source TEXT,cwd TEXT,model TEXT,updated_at_ms INTEGER,first_user_message TEXT,archived INTEGER);INSERT INTO threads VALUES('01a06712-3498-74b3-b787-532466bf8eb8','Private title','cli','/private/project','gpt',100000,'private message',0);").unwrap();
    let history = rusqlite::Connection::open(dir.path().join("thread_history_1.sqlite")).unwrap();
    history.execute_batch("CREATE TABLE thread_turns(thread_id TEXT,turn_id TEXT,status TEXT,started_at INTEGER,completed_at INTEGER);").unwrap();
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let mut c = Config {
        agent_id: "test-agent".into(),
        ntp_server: "127.0.0.1".into(),
        ntp_timeout: 1,
        ..Default::default()
    };
    c.mqtt.url = format!("mqtt://{}", listener.local_addr().unwrap());
    c.codex.home = dir.path().to_string_lossy().into_owned();
    c.codex.rate_limits = false;
    c.codex.interval = 1;
    let status = Arc::new(Mutex::new(Status::default()));
    let cancel = CancellationToken::new();
    let task = tokio::spawn(run(c, Secrets::default(), status.clone(), cancel.clone()));
    let mut first = Peer::connect(&listener).await;
    let announcement = first.publication("/state").await;
    assert!(announcement.retain);
    let state = wire::AgentState::decode(announcement.payload).unwrap();
    let message = first.publication("/observations/codex").await;
    assert!(!message.retain);
    let observation = wire::Observation::decode(message.payload).unwrap();
    assert_eq!(observation.agent_epoch, state.agent_epoch);
    let Some(wire::observation::Payload::Codex(value)) = observation.payload else {
        panic!()
    };
    assert_eq!(value.total_count, 1);
    assert!(value.sessions[0].display_name.is_empty());
    assert!(value.sessions[0].project_name.is_empty());
    drop(first);
    let mut second = Peer::connect(&listener).await;
    let state2 = wire::AgentState::decode(second.publication("/state").await.payload).unwrap();
    assert_ne!(state2.agent_epoch, state.agent_epoch);
    let observation2 =
        wire::Observation::decode(second.publication("/observations/codex").await.payload).unwrap();
    assert_eq!(observation2.agent_epoch, state2.agent_epoch);
    let now = chrono::Utc::now().timestamp();
    let command = wire::Command {
        metadata: Some(metadata("core", 1, now, Some(20))),
        command_id: "open-disabled".into(),
        target_agent_id: "test-agent".into(),
        intent_ref: Some(wire::IntentRef {
            intent_id: "intent".into(),
            requester_kind: 1,
            requester_id: "node".into(),
        }),
        action: Some(wire::command::Action::OpenCodexSession(
            wire::OpenCodexSession {
                session_id: "01a06712-3498-74b3-b787-532466bf8eb8".into(),
            },
        )),
        ..Default::default()
    };
    let mut packet = Publish::new(
        "orbit/v1/agents/test-agent/commands",
        QoS::AtLeastOnce,
        command.encode_to_vec(),
        None,
    );
    packet.pkid = 99;
    second.send(Packet::Publish(packet.clone())).await;
    let result = wire::CommandResult::decode(second.publication("/results").await.payload).unwrap();
    assert_eq!(result.status, 3);
    assert_eq!(result.error_code, "unsupported_action");
    packet.pkid = 100;
    second.send(Packet::Publish(packet)).await;
    let duplicate =
        wire::CommandResult::decode(second.publication("/results").await.payload).unwrap();
    assert_eq!(result, duplicate);
    // A real broker sends a reason code plus zero property length.
    second
        .socket
        .write_all(&[0xe0, 0x02, 0x8e, 0x00])
        .await
        .unwrap();
    task.await.unwrap();
    let status = status.lock().unwrap();
    assert!(!status.running);
    assert!(!status.connected);
    assert!(status.message.contains("另一实例"));
}
