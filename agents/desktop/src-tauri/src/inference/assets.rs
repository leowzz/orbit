use serde::Serialize;
use sha2::{Digest, Sha256};
use std::{
    fs,
    io::{self, Read, Write},
    path::{Path, PathBuf},
};

pub const SPEECH_VERSION: &str = "0.8.11";
pub const MODEL_ID: &str = "qwen3-asr-0.6b-q8_0";
pub const MODEL_NAME: &str = "Qwen3-ASR 0.6B Q8_0";
pub const MODEL_FILE: &str = "Qwen3-ASR-0.6B-Q8_0.gguf";
pub const MODEL_REVISION: &str = "ee78f8709547f39bea3b924a16c72403826cac92";
pub const MODEL_SHA256: &str = "416e10c15b4a3d9002bd337d18fc450233fdf68502b6e10d1379d2789838afd0";
pub const MODEL_SIZE: u64 = 841_502_336;
pub const MODEL_URL: &str = "https://huggingface.co/sakasegawa/Qwen3-ASR-0.6B-GGUF/resolve/ee78f8709547f39bea3b924a16c72403826cac92/Qwen3-ASR-0.6B-Q8_0.gguf?download=true";

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ModelView {
    pub id: String,
    pub name: String,
    pub filename: String,
    pub revision: String,
    pub url: String,
    pub size: u64,
    pub downloaded: u64,
    pub installed: bool,
    pub active: bool,
    pub path: String,
    pub runtime_path: String,
    pub runtime_installed: bool,
}

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

pub fn managed_root() -> PathBuf {
    dirs::data_dir()
        .unwrap_or_else(|| PathBuf::from("."))
        .join("Orbit")
        .join("inference")
}

pub fn managed_model_path() -> PathBuf {
    managed_root().join("models").join(MODEL_FILE)
}

pub fn managed_runtime_path() -> PathBuf {
    managed_root().join("runtime").join("speech.dll")
}

pub fn model_spec(id: &str) -> Option<AssetSpec> {
    (id == MODEL_ID).then_some(AssetKind::Qwen3Asr06bQ8.spec())
}

pub fn model_view(
    selected_id: &str,
    selected_path: &str,
    selected_runtime_path: &str,
) -> ModelView {
    let path = if selected_id == MODEL_ID && !selected_path.is_empty() {
        PathBuf::from(selected_path)
    } else {
        managed_model_path()
    };
    let downloaded = fs::metadata(&path).map(|m| m.len()).unwrap_or(0);
    let installed = downloaded == MODEL_SIZE;
    let runtime_path = if selected_runtime_path.is_empty() {
        managed_runtime_path()
    } else {
        PathBuf::from(selected_runtime_path)
    };
    ModelView {
        id: MODEL_ID.into(),
        name: MODEL_NAME.into(),
        filename: MODEL_FILE.into(),
        revision: MODEL_REVISION.into(),
        url: MODEL_URL.into(),
        size: MODEL_SIZE,
        downloaded,
        installed,
        active: selected_id == MODEL_ID && selected_path == path.to_string_lossy().as_ref(),
        path: path.to_string_lossy().into_owned(),
        runtime_path: runtime_path.to_string_lossy().into_owned(),
        runtime_installed: runtime_path.is_file(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn catalog_uses_fixed_qwen_asset() {
        let spec = model_spec(MODEL_ID).unwrap();
        assert_eq!(spec.expected_size, Some(MODEL_SIZE));
        assert_eq!(spec.sha256, MODEL_SHA256);
        assert!(model_spec("unknown").is_none());
    }

    #[test]
    fn model_view_keeps_configured_paths() {
        let view = model_view(MODEL_ID, r"C:\models\qwen.gguf", r"C:\runtime\speech.dll");
        assert!(view.active);
        assert_eq!(view.path, r"C:\models\qwen.gguf");
        assert_eq!(view.runtime_path, r"C:\runtime\speech.dll");
    }
}
