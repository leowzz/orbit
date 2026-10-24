//! Narrow, versioned speech.cpp v0.8.11 C ABI wrapper.
//!
//! The dynamic library is loaded from an explicit configured path.  No symbol
//! is resolved through the process search path and no raw handle escapes this
//! module.  Model and request lifetimes mirror the C API's required order.

use libloading::Library;
use std::{
    ffi::{CStr, CString},
    path::Path,
    ptr::NonNull,
};

const API_MAJOR: i32 = 3;
const API_MINOR: i32 = 3;

#[repr(C)]
struct LoadParams {
    _private: [u8; 0],
}
#[repr(C)]
struct Model {
    _private: [u8; 0],
}
#[repr(C)]
struct Request {
    _private: [u8; 0],
}
#[repr(C)]
struct ResultHandle {
    _private: [u8; 0],
}

type Status = i32;
type ApiVersion = unsafe extern "C" fn() -> i32;
type LastError = unsafe extern "C" fn() -> *const std::ffi::c_char;
type ParamsNew = unsafe extern "C" fn(*mut *mut LoadParams) -> Status;
type ParamsFree = unsafe extern "C" fn(*mut LoadParams);
type ParamsSetDevice = unsafe extern "C" fn(*mut LoadParams, *const std::ffi::c_char) -> Status;
type ModelLoad =
    unsafe extern "C" fn(*const std::ffi::c_char, *const LoadParams, *mut *mut Model) -> Status;
type ModelFree = unsafe extern "C" fn(*mut Model);
type RequestNew = unsafe extern "C" fn(*mut Model, *mut *mut Request) -> Status;
type RequestFree = unsafe extern "C" fn(*mut Request);
type RequestSetAudio = unsafe extern "C" fn(*mut Request, *const f32, usize, i32) -> Status;
type Transcribe = unsafe extern "C" fn(*mut Request) -> Status;
type Cancel = unsafe extern "C" fn(*mut Request);
type RequestResult = unsafe extern "C" fn(*const Request) -> *const ResultHandle;
type ResultText = unsafe extern "C" fn(*const ResultHandle) -> *const std::ffi::c_char;
type ResultLanguageCount = unsafe extern "C" fn(*const ResultHandle) -> usize;
type ResultLanguage = unsafe extern "C" fn(*const ResultHandle, usize) -> *const std::ffi::c_char;

#[derive(Debug, thiserror::Error)]
pub enum EngineError {
    #[error("speech runtime file not found")]
    MissingRuntime,
    #[error("speech runtime could not be loaded")]
    LoadLibrary,
    #[error("unsupported speech runtime API")]
    ApiVersion,
    #[error("speech runtime is missing a required symbol")]
    MissingSymbol,
    #[error("speech runtime call failed: {0}")]
    Call(&'static str),
    #[error("speech runtime returned invalid text")]
    InvalidText,
    #[error("speech request was cancelled")]
    Cancelled,
}

pub struct SpeechApi {
    _library: Library,
    last_error: LastError,
    params_new: ParamsNew,
    params_free: ParamsFree,
    params_set_device: ParamsSetDevice,
    model_load: ModelLoad,
    model_free: ModelFree,
    request_new: RequestNew,
    request_free: RequestFree,
    request_set_audio: RequestSetAudio,
    transcribe: Transcribe,
    #[allow(dead_code)]
    cancel: Cancel,
    request_result: RequestResult,
    result_text: ResultText,
    result_language_count: ResultLanguageCount,
    result_language: ResultLanguage,
}

unsafe fn symbol<T: Copy>(library: &Library, name: &[u8]) -> Result<T, EngineError> {
    library
        .get::<T>(name)
        .map(|s| *s)
        .map_err(|_| EngineError::MissingSymbol)
}

impl SpeechApi {
    pub fn load(path: impl AsRef<Path>) -> Result<Self, EngineError> {
        let path = path.as_ref();
        if !path.is_file() {
            return Err(EngineError::MissingRuntime);
        }
        // SAFETY: the library remains owned by SpeechApi until every copied
        // function pointer and all model/request handles have been dropped.
        let library = unsafe { Library::new(path) }.map_err(|_| EngineError::LoadLibrary)?;
        let major: ApiVersion = unsafe { symbol(&library, b"speech_api_version_major\0")? };
        let minor: ApiVersion = unsafe { symbol(&library, b"speech_api_version_minor\0")? };
        let last_error = unsafe { symbol(&library, b"speech_last_error\0")? };
        // SAFETY: version functions have no arguments and are required symbols.
        if unsafe { major() } != API_MAJOR || unsafe { minor() } < API_MINOR {
            return Err(EngineError::ApiVersion);
        }
        Ok(Self {
            params_new: unsafe { symbol(&library, b"speech_load_params_new\0")? },
            params_free: unsafe { symbol(&library, b"speech_load_params_free\0")? },
            params_set_device: unsafe { symbol(&library, b"speech_load_params_set_device\0")? },
            model_load: unsafe { symbol(&library, b"speech_model_load\0")? },
            model_free: unsafe { symbol(&library, b"speech_model_free\0")? },
            request_new: unsafe { symbol(&library, b"speech_request_new\0")? },
            request_free: unsafe { symbol(&library, b"speech_request_free\0")? },
            request_set_audio: unsafe { symbol(&library, b"speech_request_set_audio\0")? },
            transcribe: unsafe { symbol(&library, b"speech_transcribe\0")? },
            cancel: unsafe { symbol(&library, b"speech_request_cancel\0")? },
            request_result: unsafe { symbol(&library, b"speech_request_result\0")? },
            result_text: unsafe { symbol(&library, b"speech_result_text\0")? },
            result_language_count: unsafe { symbol(&library, b"speech_result_language_count\0")? },
            result_language: unsafe { symbol(&library, b"speech_result_language\0")? },
            _library: library,
            last_error,
        })
    }

    fn error(&self, operation: &'static str) -> EngineError {
        // The native message may include local paths or model metadata. Keep
        // it inside diagnostics and expose only the controlled operation code.
        let _ = self.last_error;
        EngineError::Call(operation)
    }

    pub fn load_model(
        self: std::sync::Arc<Self>,
        model_path: impl AsRef<Path>,
        device: &str,
    ) -> Result<SpeechModel, EngineError> {
        let path = CString::new(model_path.as_ref().to_string_lossy().as_bytes())
            .map_err(|_| EngineError::Call("model_path"))?;
        let device = CString::new(device).map_err(|_| EngineError::Call("device"))?;
        let mut params = std::ptr::null_mut();
        if unsafe { (self.params_new)(&mut params) } != 0 {
            return Err(self.error("load_params"));
        }
        let params_guard = ParamsGuard {
            api: self.clone(),
            ptr: NonNull::new(params).ok_or(EngineError::Call("load_params"))?,
        };
        if unsafe { (self.params_set_device)(params_guard.ptr.as_ptr(), device.as_ptr()) } != 0 {
            return Err(self.error("device"));
        }
        let mut model = std::ptr::null_mut();
        if unsafe { (self.model_load)(path.as_ptr(), params_guard.ptr.as_ptr(), &mut model) } != 0 {
            return Err(self.error("model"));
        }
        Ok(SpeechModel {
            api: self,
            ptr: NonNull::new(model).ok_or(EngineError::Call("model"))?,
        })
    }
}

struct ParamsGuard {
    api: std::sync::Arc<SpeechApi>,
    ptr: NonNull<LoadParams>,
}
impl Drop for ParamsGuard {
    fn drop(&mut self) {
        unsafe { (self.api.params_free)(self.ptr.as_ptr()) }
    }
}

pub struct SpeechModel {
    api: std::sync::Arc<SpeechApi>,
    ptr: NonNull<Model>,
}
// speech.cpp serializes requests made through one model.  The worker keeps one
// model and one request at a time, so moving the opaque handle to its dedicated
// blocking task is safe; the library owns all pointed-to allocations.
unsafe impl Send for SpeechModel {}
unsafe impl Sync for SpeechModel {}
impl SpeechModel {
    pub fn transcribe(&self, samples: &[f32], sample_rate: i32) -> Result<Transcript, EngineError> {
        if samples.is_empty() || sample_rate <= 0 {
            return Err(EngineError::Call("audio"));
        }
        let mut request = std::ptr::null_mut();
        if unsafe { (self.api.request_new)(self.ptr.as_ptr(), &mut request) } != 0 {
            return Err(self.api.error("request"));
        }
        let request = RequestGuard {
            api: self.api.clone(),
            ptr: NonNull::new(request).ok_or(EngineError::Call("request"))?,
        };
        if unsafe {
            (self.api.request_set_audio)(
                request.ptr.as_ptr(),
                samples.as_ptr(),
                samples.len(),
                sample_rate,
            )
        } != 0
        {
            return Err(self.api.error("audio"));
        }
        let status = unsafe { (self.api.transcribe)(request.ptr.as_ptr()) };
        if status == 1 {
            return Err(EngineError::Cancelled);
        }
        if status != 0 {
            return Err(self.api.error("transcribe"));
        }
        let result = unsafe { (self.api.request_result)(request.ptr.as_ptr()) };
        let result =
            NonNull::new(result as *mut ResultHandle).ok_or(EngineError::Call("result"))?;
        let text_ptr = unsafe { (self.api.result_text)(result.as_ptr()) };
        if text_ptr.is_null() {
            return Err(EngineError::InvalidText);
        }
        let text = unsafe { CStr::from_ptr(text_ptr) }
            .to_str()
            .map_err(|_| EngineError::InvalidText)?
            .to_owned();
        let mut languages = Vec::new();
        for i in 0..unsafe { (self.api.result_language_count)(result.as_ptr()) } {
            let ptr = unsafe { (self.api.result_language)(result.as_ptr(), i) };
            if !ptr.is_null() {
                languages.push(
                    unsafe { CStr::from_ptr(ptr) }
                        .to_str()
                        .map_err(|_| EngineError::InvalidText)?
                        .to_owned(),
                );
            }
        }
        Ok(Transcript {
            text,
            language: languages.into_iter().next().unwrap_or_default(),
        })
    }
}
impl Drop for SpeechModel {
    fn drop(&mut self) {
        unsafe { (self.api.model_free)(self.ptr.as_ptr()) }
    }
}

struct RequestGuard {
    api: std::sync::Arc<SpeechApi>,
    ptr: NonNull<Request>,
}
impl Drop for RequestGuard {
    fn drop(&mut self) {
        unsafe { (self.api.request_free)(self.ptr.as_ptr()) }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Transcript {
    pub text: String,
    pub language: String,
}
