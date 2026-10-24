use libloading::Library;
use std::{
    ffi::c_void,
    path::{Path, PathBuf},
    ptr::NonNull,
};

type Create = unsafe extern "C" fn(i32, i32, *mut i32) -> *mut c_void;
type Destroy = unsafe extern "C" fn(*mut c_void);
type Decode = unsafe extern "C" fn(*mut c_void, *const u8, i32, *mut i16, i32, i32) -> i32;

pub struct Decoder {
    _library: Library,
    decoder: NonNull<c_void>,
    destroy: Destroy,
    decode: Decode,
}

unsafe impl Send for Decoder {}

impl Decoder {
    pub fn load(runtime_path: &Path) -> Result<Self, String> {
        let mut candidates = Vec::new();
        if let Some(parent) = runtime_path.parent() {
            candidates.push(parent.join("opus.dll"));
            candidates.push(parent.join("libopus.dll"));
            if let Ok(entries) = std::fs::read_dir(parent) {
                for entry in entries.flatten() {
                    let path = entry.path();
                    if path
                        .extension()
                        .is_some_and(|v| v.eq_ignore_ascii_case("dll"))
                        && path.file_name().is_some_and(|v| {
                            v.to_string_lossy().to_ascii_lowercase().contains("opus")
                        })
                    {
                        candidates.push(path);
                    }
                }
            }
        }
        candidates.push(PathBuf::from("opus.dll"));
        candidates.push(PathBuf::from("libopus.dll"));
        let path = candidates
            .into_iter()
            .find(|path| path.is_file())
            .ok_or_else(|| "opus runtime file not found".to_string())?;
        let library =
            unsafe { Library::new(path) }.map_err(|_| "opus runtime could not be loaded")?;
        let create: Create = unsafe {
            *library
                .get(b"opus_decoder_create\0")
                .map_err(|_| "opus runtime symbol missing")?
        };
        let destroy: Destroy = unsafe {
            *library
                .get(b"opus_decoder_destroy\0")
                .map_err(|_| "opus runtime symbol missing")?
        };
        let decode: Decode = unsafe {
            *library
                .get(b"opus_decode\0")
                .map_err(|_| "opus runtime symbol missing")?
        };
        let mut error = 0;
        let decoder = NonNull::new(unsafe { create(16_000, 1, &mut error) })
            .ok_or_else(|| format!("opus decoder init failed ({error})"))?;
        Ok(Self {
            _library: library,
            decoder,
            destroy,
            decode,
        })
    }

    pub fn decode(&mut self, packet: &[u8], output: &mut [i16]) -> Result<usize, String> {
        if packet.is_empty() || packet.len() > i32::MAX as usize || output.len() < 320 {
            return Err("opus packet or output size invalid".into());
        }
        let samples = unsafe {
            (self.decode)(
                self.decoder.as_ptr(),
                packet.as_ptr(),
                packet.len() as i32,
                output.as_mut_ptr(),
                output.len() as i32,
                0,
            )
        };
        if samples < 0 {
            Err("opus decode failed".into())
        } else {
            Ok(samples as usize)
        }
    }
}

impl Drop for Decoder {
    fn drop(&mut self) {
        unsafe { (self.destroy)(self.decoder.as_ptr()) }
    }
}
