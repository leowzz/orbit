use crate::{codex::ts, wire::SystemObservation};
use sysinfo::{CpuRefreshKind, MemoryRefreshKind, RefreshKind, System};

pub const MINIMUM_CPU_PRIME_INTERVAL: std::time::Duration = sysinfo::MINIMUM_CPU_UPDATE_INTERVAL;

/// Lightweight host CPU, memory, swap, load, and uptime collector backed by a
/// single reused `sysinfo::System` instance (no process, disk, or network scans).
pub struct Collector {
    sys: System,
}

impl Default for Collector {
    fn default() -> Self {
        Self::new()
    }
}

impl Collector {
    pub fn new() -> Self {
        let mut sys = System::new_with_specifics(
            RefreshKind::nothing()
                .with_cpu(CpuRefreshKind::nothing().with_cpu_usage())
                .with_memory(MemoryRefreshKind::everything()),
        );
        // Prime the initial CPU delta baseline before the first measurement tick.
        sys.refresh_cpu_usage();
        Self { sys }
    }

    pub fn collect(&mut self, now: i64) -> Result<SystemObservation, String> {
        self.sys.refresh_cpu_usage();
        self.sys.refresh_memory();

        let total_memory = self.sys.total_memory();
        if total_memory == 0 {
            return Err("无法读取本机内存信息".into());
        }
        let used_memory = self.sys.used_memory().min(total_memory);
        let total_swap = self.sys.total_swap();
        let used_swap = self.sys.used_swap().min(total_swap);
        let cpu_pct = self.sys.global_cpu_usage();
        let cpu_permille = if cpu_pct.is_finite() {
            (cpu_pct.clamp(0.0, 100.0) * 10.0).round() as u32
        } else {
            0
        };
        let cpu_cores = self.sys.cpus().len() as u32;

        #[cfg(windows)]
        let load_1m_milli = None;
        #[cfg(not(windows))]
        let load_1m_milli = {
            let one = System::load_average().one;
            if one.is_finite() && one >= 0.0 {
                Some((one * 1000.0).round() as u32)
            } else {
                None
            }
        };

        Ok(SystemObservation {
            observed_at: ts(now),
            cpu_usage_permille: Some(cpu_permille.min(1000)),
            cpu_cores: Some(cpu_cores),
            memory_used_bytes: Some(used_memory),
            memory_total_bytes: Some(total_memory),
            swap_used_bytes: Some(used_swap),
            swap_total_bytes: Some(total_swap),
            load_1m_milli,
            uptime_seconds: Some(System::uptime()),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn collects_valid_host_cpu_and_memory() {
        let mut collector = Collector::new();
        std::thread::sleep(MINIMUM_CPU_PRIME_INTERVAL);
        let obs = collector.collect(1_792_000_000).expect("collect system");
        assert_eq!(
            obs.observed_at.as_ref().map(|t| t.seconds),
            Some(1_792_000_000)
        );
        assert!(obs.cpu_cores.unwrap_or(0) >= 1);
        assert!(obs.cpu_usage_permille.unwrap_or(u32::MAX) <= 1000);
        let total_mem = obs.memory_total_bytes.unwrap_or(0);
        let used_mem = obs.memory_used_bytes.unwrap_or(u64::MAX);
        assert!(total_mem > 0);
        assert!(used_mem <= total_mem);
        assert!(obs.swap_used_bytes.unwrap_or(0) <= obs.swap_total_bytes.unwrap_or(0));
        assert!(obs.uptime_seconds.is_some());
        #[cfg(not(windows))]
        assert!(obs.load_1m_milli.is_some());
        #[cfg(windows)]
        assert!(obs.load_1m_milli.is_none());
    }
}
