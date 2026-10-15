// Read-only local smoke check. Does not connect to MQTT or print session content.
use orbit_desktop_agent::{codex, config::Codex};
#[tokio::main]
async fn main() -> Result<(), String> {
    let config = Codex::default();
    let now = chrono::Utc::now().timestamp();
    let value = codex::collect(&config, now)?;
    assert!(value
        .sessions
        .iter()
        .all(|s| s.display_name.is_empty() && s.project_name.is_empty()));
    println!(
        "Codex collection OK: {} total, {} running, {} displayed; private fields redacted",
        value.total_count,
        value.running_count,
        value.sessions.len()
    );
    if std::env::args().any(|a| a == "--quota") {
        let value = codex::weekly(&config, now).await?;
        assert!((0.0..=100.0).contains(&value.remaining_percent));
        println!("Codex weekly quota RPC OK");
    }
    Ok(())
}
