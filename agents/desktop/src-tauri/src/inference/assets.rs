use sha2::{Digest, Sha256};
use std::{
    fs,
    io::{self, Read, Write},
    path::{Path, PathBuf},
};

pub const SPEECH_VERSION: &str = "0.8.11";
pub const MODEL_REVISION: &str = "ee78f8709547f39bea3b924a16c72403826cac92";
pub const MODEL_SHA256: &str = "416e10c15b4a3d9002bd337d18fc450233fdf68502b6e10d1379d2789838afd0";
pub const MODEL_SIZE: u64 = 841_502_336;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AssetKind {
    SpeechWindowsX64Vulkan,
    SpeechMacosArm64Metal,
    Qwen3Asr06bQ8,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct AssetSpec {
    pub kind: AssetKind,
    pub expected_size: Option<u64>,
    pub sha256: &'static str,
}

impl AssetKind {
    pub fn spec(self) -> AssetSpec {
        match self {
            Self::SpeechWindowsX64Vulkan => AssetSpec {
                kind: self,
                expected_size: Some(28_886_830),
                sha256: "63f4c5b5c60aeab9d4c90193298fe4ee5d2a6c931cdbdcbc8cbb126fbffc56d0",
            },
            Self::SpeechMacosArm64Metal => AssetSpec {
                kind: self,
                expected_size: None,
                sha256: "85471d762535cfdb52e962a10542bb6aa6a039f559bee62703c1eb2d274b8125",
            },
            Self::Qwen3Asr06bQ8 => AssetSpec {
                kind: self,
                expected_size: Some(MODEL_SIZE),
                sha256: MODEL_SHA256,
            },
        }
    }
}

pub fn verify(path: &Path, spec: AssetSpec) -> io::Result<()> {
    let mut file = fs::File::open(path)?;
    let size = file.metadata()?.len();
    if spec.expected_size.is_some_and(|expected| expected != size) {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "asset size mismatch",
        ));
    }
    let mut hash = Sha256::new();
    let mut buf = [0u8; 1024 * 1024];
    loop {
        let n = file.read(&mut buf)?;
        if n == 0 {
            break;
        }
        hash.update(&buf[..n]);
    }
    if hex::encode(hash.finalize()) != spec.sha256 {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "asset checksum mismatch",
        ));
    }
    Ok(())
}

pub fn install_verified(source: &Path, target: &Path, spec: AssetSpec) -> io::Result<()> {
    verify(source, spec)?;
    let parent = target
        .parent()
        .ok_or_else(|| io::Error::new(io::ErrorKind::InvalidInput, "asset target has no parent"))?;
    fs::create_dir_all(parent)?;
    let temp = tempfile::NamedTempFile::new_in(parent)?;
    let temp_path: PathBuf = temp.path().to_path_buf();
    let mut input = fs::File::open(source)?;
    let mut output = temp.reopen()?;
    io::copy(&mut input, &mut output)?;
    output.flush()?;
    verify(&temp_path, spec)?;
    temp.persist(target).map_err(|e| e.error)?;
    Ok(())
}
