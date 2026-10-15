use std::{
    sync::atomic::{AtomicI64, Ordering},
    time::{Duration, SystemTime, UNIX_EPOCH},
};
#[derive(Default)]
pub struct Clock {
    offset_ms: AtomicI64,
}
impl Clock {
    pub fn now_millis(&self) -> i64 {
        system_ms() + self.offset_ms.load(Ordering::Relaxed)
    }
    pub fn now(&self) -> i64 {
        self.now_millis() / 1000
    }
    pub async fn sync(&self, host: &str, timeout: u64) -> Result<(), String> {
        let offset = tokio::time::timeout(Duration::from_secs(timeout), async {
            let address = tokio::net::lookup_host((host, 123))
                .await
                .map_err(|_| "时间服务器不可用")?
                .next()
                .ok_or("时间服务器不可用")?;
            let socket = tokio::net::UdpSocket::bind(if address.is_ipv4() {
                "0.0.0.0:0"
            } else {
                "[::]:0"
            })
            .await
            .map_err(|_| "无法校准时间")?;
            socket.connect(address).await.map_err(|_| "无法校准时间")?;
            let sent = system_ms();
            let mut request = [0u8; 48];
            request[0] = 0x23;
            let stamp = ntp_stamp(sent);
            request[40..48].copy_from_slice(&stamp);
            socket.send(&request).await.map_err(|_| "无法校准时间")?;
            let mut reply = [0u8; 512];
            let count = socket.recv(&mut reply).await.map_err(|_| "无法校准时间")?;
            validate_reply(&reply[..count], &stamp, sent, system_ms())
        })
        .await
        .map_err(|_| "时间校准超时")??;
        self.offset_ms.store(offset, Ordering::Relaxed);
        Ok(())
    }
}
fn system_ms() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as i64
}
fn ntp_stamp(ms: i64) -> [u8; 8] {
    let seconds = (ms / 1000 + 2_208_988_800) as u32;
    let fraction = (((ms % 1000) as u64) << 32) / 1000;
    let mut out = [0; 8];
    out[..4].copy_from_slice(&seconds.to_be_bytes());
    out[4..].copy_from_slice(&(fraction as u32).to_be_bytes());
    out
}
fn unix_ms(bytes: &[u8]) -> i64 {
    let sec = u32::from_be_bytes(bytes[..4].try_into().unwrap()) as i64;
    let frac = u32::from_be_bytes(bytes[4..8].try_into().unwrap()) as i64;
    (sec - 2_208_988_800) * 1000 + ((frac * 1000) >> 32)
}
fn validate_reply(reply: &[u8], stamp: &[u8; 8], sent: i64, received: i64) -> Result<i64, String> {
    if reply.len() < 48
        || reply[0] >> 6 == 3
        || reply[0] & 7 != 4
        || !(3..=4).contains(&((reply[0] >> 3) & 7))
        || !(1..=15).contains(&reply[1])
        || reply[24..32] != stamp[..]
        || reply[32..40] == [0; 8]
        || reply[40..48] == [0; 8]
    {
        return Err("时间响应无效".into());
    }
    let start = unix_ms(&reply[32..40]);
    let end = unix_ms(&reply[40..48]);
    if end < start || received < sent {
        return Err("时间响应无效".into());
    }
    Ok(((start - sent) + (end - received)) / 2)
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn validates_origin_and_offset() {
        let now = 1_790_000_000_000;
        let stamp = ntp_stamp(now);
        let mut p = [0; 48];
        p[0] = 0x24;
        p[1] = 2;
        p[24..32].copy_from_slice(&stamp);
        p[32..40].copy_from_slice(&ntp_stamp(now + 1000));
        p[40..48].copy_from_slice(&ntp_stamp(now + 1000));
        assert_eq!(validate_reply(&p, &stamp, now, now + 100).unwrap(), 950);
        p[24] ^= 1;
        assert!(validate_reply(&p, &stamp, now, now + 100).is_err());
    }
}
