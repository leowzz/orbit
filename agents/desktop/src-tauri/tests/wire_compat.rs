use orbit_desktop_agent::{
    commands::validate,
    wire::{observation::Payload, Command, Observation},
};
use prost::Message;
#[test]
fn go_command_decodes_and_roundtrips() {
    let bytes = include_bytes!("../../testdata/go-command.bin");
    let command = Command::decode(&bytes[..]).unwrap();
    assert_eq!(
        validate(&command, "fixture-agent", true, 1792000001).unwrap(),
        "codex://threads/01a06712-3498-74b3-b787-532466bf8eb8"
    );
    assert_eq!(command.encode_to_vec(), bytes);
}
#[test]
fn go_optional_zeroes_and_micros_roundtrip() {
    let bytes = include_bytes!("../../testdata/go-observation.bin");
    let observation = Observation::decode(&bytes[..]).unwrap();
    let Some(Payload::Usage(ref usage)) = observation.payload else {
        panic!()
    };
    assert_eq!(usage.token_count, Some(0));
    assert_eq!(usage.actual_cost_micros, Some(1234567));
    assert_eq!(observation.encode_to_vec(), bytes);
}
#[test]
fn go_system_observation_zeroes_roundtrip() {
    let bytes = include_bytes!("../../testdata/go-system-observation.bin");
    let observation = Observation::decode(&bytes[..]).unwrap();
    let Some(Payload::System(ref system)) = observation.payload else {
        panic!()
    };
    assert_eq!(system.cpu_usage_permille, Some(0));
    assert_eq!(system.cpu_cores, Some(8));
    assert_eq!(system.memory_used_bytes, Some(0));
    assert_eq!(system.memory_total_bytes, Some(32u64 << 30));
    assert_eq!(system.load_1m_milli, None);
    assert_eq!(observation.encode_to_vec(), bytes);
}
