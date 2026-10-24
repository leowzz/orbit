//! Native ASR building blocks shared by the desktop worker.
//!
//! The MQTT lifecycle and speech.cpp binding are intentionally kept above this
//! layer.  This module owns the cross-language protocol and the durable queue,
//! so a process restart cannot acknowledge work that was not persisted.

pub mod assets;
pub mod engine;
pub mod opus;
pub mod protocol;
pub mod store;
pub mod worker;
