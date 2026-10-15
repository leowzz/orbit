fn main() {
    let root = "../../../proto";
    std::env::set_var(
        "PROTOC",
        protoc_bin_vendored::protoc_bin_path().expect("bundled protoc"),
    );
    prost_build::Config::new()
        .compile_protos(
            &[
                format!("{root}/orbit/v1/state.proto"),
                format!("{root}/orbit/v1/command.proto"),
                format!("{root}/orbit/v1/navigation.proto"),
            ],
            &[
                root.to_string(),
                protoc_bin_vendored::include_path()
                    .unwrap()
                    .display()
                    .to_string(),
            ],
        )
        .expect("compile canonical Orbit schema");
    println!("cargo:rerun-if-changed={root}/orbit/v1");
    tauri_build::build();
}
