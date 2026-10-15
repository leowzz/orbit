use crate::{codex::ts, config::Usage, wire::UsageObservation};
use chrono::{Days, TimeZone, Utc};
use rust_decimal::{prelude::ToPrimitive, Decimal, RoundingStrategy};
use serde_json::{json, Value};
use std::{str::FromStr, time::Duration};

#[derive(Debug)]
pub enum Error {
    Unauthorized,
    RateLimited(Duration),
    Unavailable,
    Invalid,
}
impl Error {
    pub fn message(&self) -> String {
        match self {
            Self::Unauthorized => "Sub2API 登录失效，请检查邮箱和密码",
            Self::RateLimited(_) => "请求过于频繁，稍后自动重试",
            Self::Unavailable => "暂时无法连接 Sub2API",
            Self::Invalid => "Sub2API 返回数据格式不正确",
        }
        .into()
    }
}
pub struct Source {
    config: Usage,
    password: String,
    client: reqwest::Client,
    access: String,
    refresh: String,
}
impl Source {
    pub fn new(config: Usage, password: String) -> Result<Self, String> {
        let client = reqwest::Client::builder()
            .timeout(Duration::from_secs(config.timeout))
            .redirect(reqwest::redirect::Policy::none())
            .build()
            .map_err(|_| "无法初始化用量采集")?;
        Ok(Self {
            config,
            password,
            client,
            access: String::new(),
            refresh: String::new(),
        })
    }
    async fn request(&self, url: &str, body: Option<Value>, token: &str) -> Result<Value, Error> {
        let mut request = if let Some(value) = body {
            self.client.post(url).json(&value)
        } else {
            self.client.get(url)
        };
        if !token.is_empty() {
            request = request.bearer_auth(token)
        }
        let mut response = request.send().await.map_err(|_| Error::Unavailable)?;
        let code = response.status().as_u16();
        if code == 401 {
            return Err(Error::Unauthorized);
        }
        if code == 429 {
            let delay = response
                .headers()
                .get("Retry-After")
                .and_then(|v| v.to_str().ok())
                .and_then(|v| {
                    v.parse::<u64>().ok().or_else(|| {
                        chrono::DateTime::parse_from_rfc2822(v)
                            .ok()
                            .map(|t| (t.timestamp() - Utc::now().timestamp()).max(1) as u64)
                    })
                })
                .unwrap_or(60);
            return Err(Error::RateLimited(Duration::from_secs(delay.min(86400))));
        }
        if !(200..300).contains(&code) {
            return Err(Error::Unavailable);
        }
        let mut bytes = vec![];
        while let Some(chunk) = response.chunk().await.map_err(|_| Error::Unavailable)? {
            if bytes.len() + chunk.len() > 8192 {
                return Err(Error::Invalid);
            }
            bytes.extend_from_slice(&chunk);
        }
        let envelope: Value = serde_json::from_slice(&bytes).map_err(|_| Error::Invalid)?;
        if envelope["code"].as_i64() != Some(0)
            || envelope["message"].as_str().is_none()
            || !envelope["data"].is_object()
        {
            return Err(Error::Invalid);
        }
        Ok(envelope["data"].clone())
    }
    async fn login(&mut self) -> Result<(), Error> {
        let endpoints = self.config.endpoints();
        let v = self
            .request(
                &endpoints[0],
                Some(json!({"email":self.config.email,"password":self.password})),
                "",
            )
            .await?;
        self.tokens(v)
    }
    fn tokens(&mut self, v: Value) -> Result<(), Error> {
        let access = v["access_token"]
            .as_str()
            .filter(|s| !s.is_empty())
            .ok_or(Error::Invalid)?;
        let refresh = v["refresh_token"]
            .as_str()
            .filter(|s| !s.is_empty())
            .ok_or(Error::Invalid)?;
        if v["expires_in"].as_u64().is_none() {
            return Err(Error::Invalid);
        }
        self.access = access.into();
        self.refresh = refresh.into();
        Ok(())
    }
    pub async fn fetch(&mut self, now: i64) -> Result<UsageObservation, Error> {
        if self.access.is_empty() {
            self.login().await?;
        }
        let endpoints = self.config.endpoints();
        let mut result = self.request(&endpoints[2], None, &self.access).await;
        if matches!(result, Err(Error::Unauthorized)) {
            let refresh = self
                .request(
                    &endpoints[1],
                    Some(json!({"refresh_token":self.refresh})),
                    "",
                )
                .await;
            match refresh {
                Ok(v) => {
                    if self.tokens(v).is_err() {
                        self.login().await?
                    }
                }
                Err(e @ Error::RateLimited(_)) => return Err(e),
                Err(_) => self.login().await?,
            }
            result = self.request(&endpoints[2], None, &self.access).await;
        }
        parse_usage(&result?, &self.config.timezone, now)
    }
}
pub fn cost_micros(v: &Value) -> Result<i64, Error> {
    if !v.is_number() {
        return Err(Error::Invalid);
    }
    let text = v.to_string();
    let n = Decimal::from_str(&text)
        .or_else(|_| Decimal::from_scientific(&text))
        .map_err(|_| Error::Invalid)?;
    if n.is_sign_negative() {
        return Err(Error::Invalid);
    }
    n.checked_mul(Decimal::from(1_000_000))
        .ok_or(Error::Invalid)?
        .round_dp_with_strategy(0, RoundingStrategy::MidpointAwayFromZero)
        .to_i64()
        .ok_or(Error::Invalid)
}
fn parse_usage(v: &Value, timezone: &str, now: i64) -> Result<UsageObservation, Error> {
    let zone = timezone
        .parse::<chrono_tz::Tz>()
        .map_err(|_| Error::Invalid)?;
    let local = Utc
        .timestamp_opt(now, 0)
        .single()
        .ok_or(Error::Invalid)?
        .with_timezone(&zone);
    let date = local.date_naive();
    let start = zone
        .from_local_datetime(&date.and_hms_opt(0, 0, 0).ok_or(Error::Invalid)?)
        .earliest()
        .ok_or(Error::Invalid)?;
    let end = zone
        .from_local_datetime(
            &date
                .checked_add_days(Days::new(1))
                .ok_or(Error::Invalid)?
                .and_hms_opt(0, 0, 0)
                .ok_or(Error::Invalid)?,
        )
        .earliest()
        .ok_or(Error::Invalid)?;
    Ok(UsageObservation {
        window_start: ts(start.timestamp()),
        window_end: ts(end.timestamp()),
        actual_cost_micros: Some(cost_micros(&v["today_actual_cost"])?),
        currency_code: "USD".into(),
        token_count: Some(v["today_tokens"].as_u64().ok_or(Error::Invalid)?),
        tpm: Some(v["tpm"].as_u64().ok_or(Error::Invalid)?),
        observed_at: ts(now),
    })
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn exact_rounding_and_invalid_cost() {
        assert_eq!(
            cost_micros(&serde_json::from_str("1.2345675").unwrap()).unwrap(),
            1234568
        );
        assert_eq!(
            cost_micros(&serde_json::from_str("1e-6").unwrap()).unwrap(),
            1
        );
        for v in [json!(-1), json!("2"), json!(null)] {
            assert!(cost_micros(&v).is_err());
        }
    }
    #[test]
    fn local_window_and_zeroes() {
        let now = chrono::DateTime::parse_from_rfc3339("2026-10-15T00:30:00+08:00")
            .unwrap()
            .timestamp();
        let v = parse_usage(
            &json!({"today_actual_cost":0,"today_tokens":0,"tpm":0}),
            "Asia/Shanghai",
            now,
        )
        .unwrap();
        assert_eq!(
            v.window_end.unwrap().seconds - v.window_start.unwrap().seconds,
            86400
        );
        assert_eq!(v.token_count, Some(0));
        assert!(parse_usage(&json!({"today_actual_cost":0}), "Asia/Shanghai", now).is_err());
    }
}
