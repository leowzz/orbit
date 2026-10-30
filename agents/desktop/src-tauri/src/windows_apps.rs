//! Windows application discovery and metadata. All calls run off the UI thread.
use std::{
    collections::HashMap,
    ffi::OsStr,
    mem::{size_of, zeroed},
    os::windows::ffi::OsStrExt,
    path::Path,
    ptr::{null, null_mut},
};
use windows_sys::Win32::{
    Foundation::{CloseHandle, FILETIME, HWND, LPARAM},
    Globalization::GetUserDefaultUILanguage,
    Graphics::{Dwm::*, Gdi::*},
    Storage::FileSystem::{GetFileVersionInfoSizeW, GetFileVersionInfoW, VerQueryValueW},
    System::{Com::*, Threading::*},
    UI::{Shell::*, WindowsAndMessaging::*},
};

fn wide(value: impl AsRef<OsStr>) -> Vec<u16> {
    value.as_ref().encode_wide().chain(Some(0)).collect()
}

// Shell APIs do not consistently support the extended prefix from canonicalize().
pub(crate) fn shell_path(path: &str) -> String {
    if let Some(rest) = path.strip_prefix(r"\\?\UNC\") {
        format!(r"\\{rest}")
    } else {
        path.strip_prefix(r"\\?\").unwrap_or(path).to_string()
    }
}

pub(crate) fn path_key(path: &str) -> String {
    shell_path(path).replace('/', "\\").to_lowercase()
}

struct Windows {
    foreground: HWND,
    pids: HashMap<u32, bool>,
}

unsafe extern "system" fn collect_window(window: HWND, data: LPARAM) -> i32 {
    // EnumWindows invokes this callback synchronously with our stack-owned state.
    let state = &mut *(data as *mut Windows);
    if IsWindowVisible(window) == 0 || window == GetShellWindow() {
        return 1;
    }
    let style = GetWindowLongPtrW(window, GWL_EXSTYLE) as u32;
    if style & (WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE) != 0 {
        return 1;
    }
    let mut cloaked: u32 = 0;
    if DwmGetWindowAttribute(
        window,
        DWMWA_CLOAKED as u32,
        (&mut cloaked as *mut u32).cast(),
        size_of::<u32>() as u32,
    ) >= 0
        && cloaked != 0
    {
        return 1;
    }
    let mut pid = 0;
    GetWindowThreadProcessId(window, &mut pid);
    if pid != 0 && pid != std::process::id() {
        let front = state.pids.entry(pid).or_default();
        *front |= window == state.foreground;
    }
    1
}

fn process(pid: u32) -> Option<(String, i64)> {
    unsafe {
        let handle = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid);
        if handle.is_null() {
            return None;
        }
        let mut path = vec![0u16; 32768];
        let mut len = path.len() as u32;
        let found = QueryFullProcessImageNameW(handle, 0, path.as_mut_ptr(), &mut len);
        let mut created: FILETIME = zeroed();
        let mut exited = zeroed();
        let mut kernel = zeroed();
        let mut user = zeroed();
        let timed = GetProcessTimes(handle, &mut created, &mut exited, &mut kernel, &mut user);
        CloseHandle(handle);
        if found == 0 {
            return None;
        }
        let path = String::from_utf16_lossy(&path[..len as usize]);
        if !crate::shortcuts::app_path(Path::new(&path)) {
            return None;
        }
        let ticks = ((created.dwHighDateTime as u64) << 32) | created.dwLowDateTime as u64;
        let launched = if timed != 0 {
            (ticks / 10_000_000) as i64 - 11_644_473_600
        } else {
            0
        };
        Some((path, launched))
    }
}

pub(crate) type Snapshot = (Vec<(String, i64)>, Option<String>);

pub(crate) fn sample() -> Result<Snapshot, std::io::Error> {
    let mut state = Windows {
        foreground: unsafe { GetForegroundWindow() },
        pids: HashMap::new(),
    };
    if unsafe { EnumWindows(Some(collect_window), (&mut state as *mut Windows) as LPARAM) } == 0 {
        return Err(std::io::Error::last_os_error());
    }
    let mut apps = HashMap::<String, (String, i64)>::new();
    let mut front = None;
    for (pid, active) in state.pids {
        if let Some((path, launched)) = process(pid) {
            if active {
                front = Some(path.clone());
            }
            // Multiple windows/processes of one executable yield a single application.
            let entry = apps.entry(path_key(&path)).or_insert((path, launched));
            entry.1 = entry.1.max(launched);
        }
    }
    Ok((apps.into_values().collect(), front))
}

fn utf16_text(value: &[u16]) -> String {
    String::from_utf16_lossy(&value[..value.iter().position(|c| *c == 0).unwrap_or(value.len())])
        .trim()
        .to_string()
}

fn version_title(path: &[u16]) -> Option<String> {
    unsafe {
        let size = GetFileVersionInfoSizeW(path.as_ptr(), null_mut());
        if size == 0 || size > 4 * 1024 * 1024 {
            return None;
        }
        // u32 storage keeps the version block aligned for Windows' returned pointers.
        let mut data = vec![0u32; (size as usize).div_ceil(4)];
        if GetFileVersionInfoW(path.as_ptr(), 0, size, data.as_mut_ptr().cast()) == 0 {
            return None;
        }
        let query = |key: &str| -> Option<(*const u16, usize)> {
            let mut value = null_mut();
            let mut len = 0;
            if VerQueryValueW(
                data.as_ptr().cast(),
                wide(key).as_ptr(),
                &mut value,
                &mut len,
            ) == 0
                || value.is_null()
                || len == 0
            {
                None
            } else {
                Some((value.cast(), len as usize))
            }
        };
        let mut translations = Vec::new();
        if let Some((value, bytes)) = query(r"\VarFileInfo\Translation") {
            for pair in std::slice::from_raw_parts(value, bytes / 2)
                .as_chunks::<2>()
                .0
            {
                translations.push((pair[0], pair[1]));
            }
        }
        let language = GetUserDefaultUILanguage();
        translations.sort_by_key(|(lang, _)| (*lang != language, *lang != 0x0409));
        translations.extend([(language, 1200), (0x0409, 1200), (0x0409, 1252)]);
        for field in ["FileDescription", "ProductName"] {
            for (lang, codepage) in &translations {
                if let Some((value, len)) = query(&format!(
                    r"\StringFileInfo\{lang:04x}{codepage:04x}\{field}"
                )) {
                    let title = utf16_text(std::slice::from_raw_parts(value, len));
                    if !title.is_empty() {
                        return Some(title);
                    }
                }
            }
        }
        None
    }
}

struct ComApartment(bool);
impl ComApartment {
    fn new() -> Self {
        Self(unsafe { CoInitializeEx(null(), COINIT_APARTMENTTHREADED as u32) } >= 0)
    }
}
impl Drop for ComApartment {
    fn drop(&mut self) {
        if self.0 {
            unsafe { CoUninitialize() };
        }
    }
}

pub(crate) fn metadata(path: &Path) -> (Option<String>, String) {
    let _com = ComApartment::new();
    let path_text = shell_path(&path.to_string_lossy());
    let path_wide = wide(&path_text);
    let is_link = path
        .extension()
        .is_some_and(|e| e.eq_ignore_ascii_case("lnk"));
    let mut info: SHFILEINFOW = unsafe { zeroed() };
    let found = unsafe {
        SHGetFileInfoW(
            path_wide.as_ptr(),
            0,
            &mut info,
            size_of::<SHFILEINFOW>() as u32,
            SHGFI_ICON | SHGFI_LARGEICON | SHGFI_DISPLAYNAME,
        )
    } != 0;
    // A shortcut's name and custom icon describe its launch action; keep its target/args intact.
    let title = if is_link && found {
        let name = utf16_text(&info.szDisplayName);
        let name = if name.to_lowercase().ends_with(".lnk") {
            name[..name.len() - 4].to_string()
        } else {
            name
        };
        (!name.is_empty()).then_some(name)
    } else {
        version_title(&path_wide)
    };
    // Some executables have icon resources that the Shell's file-info handler misses.
    if info.hIcon.is_null() && !is_link {
        unsafe { ExtractIconExW(path_wide.as_ptr(), 0, &mut info.hIcon, null_mut(), 1) };
    }
    let icon = if !info.hIcon.is_null() {
        let png = icon_png(info.hIcon).unwrap_or_default();
        unsafe { DestroyIcon(info.hIcon) };
        png
    } else {
        String::new()
    };
    (title, icon)
}

fn icon_png(icon: HICON) -> Option<String> {
    const SIDE: usize = 64;
    unsafe {
        let dc = CreateCompatibleDC(null_mut());
        if dc.is_null() {
            return None;
        }
        let mut info: BITMAPINFO = zeroed();
        info.bmiHeader.biSize = size_of::<BITMAPINFOHEADER>() as u32;
        info.bmiHeader.biWidth = SIDE as i32;
        info.bmiHeader.biHeight = -(SIDE as i32);
        info.bmiHeader.biPlanes = 1;
        info.bmiHeader.biBitCount = 32;
        info.bmiHeader.biCompression = BI_RGB;
        let mut bits = null_mut();
        let bitmap = CreateDIBSection(dc, &info, DIB_RGB_COLORS, &mut bits, null_mut(), 0);
        if bitmap.is_null() {
            DeleteDC(dc);
            return None;
        }
        let old = SelectObject(dc, bitmap);
        let result = (|| {
            // Render on black and white to recover alpha for both modern and legacy mask icons.
            let pixels = std::slice::from_raw_parts_mut(bits.cast::<u8>(), SIDE * SIDE * 4);
            pixels.fill(0);
            if DrawIconEx(
                dc,
                0,
                0,
                icon,
                SIDE as i32,
                SIDE as i32,
                0,
                null_mut(),
                DI_NORMAL,
            ) == 0
            {
                return None;
            }
            GdiFlush();
            let black = pixels.to_vec();
            pixels.fill(255);
            if DrawIconEx(
                dc,
                0,
                0,
                icon,
                SIDE as i32,
                SIDE as i32,
                0,
                null_mut(),
                DI_NORMAL,
            ) == 0
            {
                return None;
            }
            GdiFlush();
            let rgba = icon_rgba(&black, pixels);
            let image = image::RgbaImage::from_raw(SIDE as u32, SIDE as u32, rgba)?;
            let mut png = std::io::Cursor::new(Vec::new());
            image.write_to(&mut png, image::ImageFormat::Png).ok()?;
            crate::shortcuts::encoded_png(png.into_inner()).ok()
        })();
        SelectObject(dc, old);
        DeleteObject(bitmap);
        DeleteDC(dc);
        result
    }
}

fn icon_rgba(black: &[u8], white: &[u8]) -> Vec<u8> {
    let mut rgba = Vec::with_capacity(black.len());
    for (b, w) in black
        .as_chunks::<4>()
        .0
        .iter()
        .zip(white.as_chunks::<4>().0)
    {
        let alpha = 255 - (0..3).map(|i| w[i].saturating_sub(b[i])).max().unwrap();
        for i in [2, 1, 0] {
            rgba.push(if alpha == 0 {
                0
            } else {
                ((b[i] as u32 * 255) / alpha as u32).min(255) as u8
            });
        }
        rgba.push(alpha);
    }
    rgba
}

#[cfg(test)]
mod tests {
    use super::*;
    use base64::Engine;

    #[test]
    fn windows_paths_match_across_case_slashes_and_extended_prefixes() {
        assert_eq!(
            path_key(r"\\?\C:\Apps\Example.EXE"),
            path_key("c:/apps/example.exe")
        );
        assert_eq!(
            path_key(r"\\?\UNC\Server\Apps\A.exe"),
            path_key(r"\\server\apps\a.exe")
        );
    }

    #[test]
    fn icons_recover_transparency_color_and_legacy_masks() {
        assert_eq!(icon_rgba(&[0, 0, 0, 0], &[255, 255, 255, 0]), [0, 0, 0, 0]);
        assert_eq!(
            icon_rgba(&[10, 20, 30, 0], &[10, 20, 30, 0]),
            [30, 20, 10, 255]
        );
        assert_eq!(
            icon_rgba(&[0, 0, 128, 0], &[127, 127, 255, 0]),
            [255, 0, 0, 128]
        );
    }

    #[test]
    fn native_metadata_reads_executable_name_and_valid_png() {
        let path = std::path::PathBuf::from(std::env::var_os("SystemRoot").unwrap())
            .join("System32/cmd.exe");
        let app = crate::shortcuts::import_app(path.to_str().unwrap()).unwrap();
        assert!(!app.title.is_empty());
        assert_ne!(app.title, "cmd");
        crate::shortcuts::validate(std::slice::from_ref(&app)).unwrap();
        let bytes = base64::engine::general_purpose::STANDARD
            .decode(&app.icon)
            .unwrap();
        let image = image::load_from_memory(&bytes).unwrap().to_rgba8();
        assert_eq!(image.dimensions(), (64, 64));
        assert!(image.pixels().any(|p| p[3] > 0));
        assert!(image.pixels().any(|p| p[3] == 0));
    }

    #[test]
    fn native_discovery_samples_current_desktop() {
        let (running, front) = sample().unwrap();
        let mut seen = std::collections::HashSet::new();
        for (path, launched) in &running {
            assert!(seen.insert(path_key(path)), "duplicate executable");
            assert!(Path::new(path).is_file());
            assert!(*launched >= 0);
            let (name, icon) = metadata(Path::new(path));
            eprintln!(
                "Windows application: {} (icon: {})",
                name.unwrap_or_default(),
                !icon.is_empty()
            );
        }
        if let Some(front) = front {
            assert!(seen.contains(&path_key(&front)));
        }
    }

    #[test]
    fn native_shortcut_keeps_unicode_name_custom_icon_and_launch_action() {
        use std::os::windows::process::CommandExt;
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("测试应用 ' 中文.lnk");
        // WScript.Save uses the system code page on some Windows images.
        // Create an ASCII fixture, then rename via Rust's Unicode filesystem API
        // so the actual importer is still tested with Chinese and a quote.
        let fixture = dir.path().join("fixture.lnk");
        let output = std::process::Command::new("powershell.exe")
            .args(["-NoProfile", "-NonInteractive", "-Command",
                "$s=(New-Object -ComObject WScript.Shell).CreateShortcut($env:ORBIT_TEST_LINK); $s.TargetPath=Join-Path $env:SystemRoot 'System32/cmd.exe'; $s.Arguments='/c echo test'; $s.IconLocation=(Join-Path $env:SystemRoot 'System32/shell32.dll')+',3'; $s.Save()"])
            .env("ORBIT_TEST_LINK", &fixture)
            .creation_flags(0x08000000)
            .output().unwrap();
        assert!(
            output.status.success(),
            "shortcut fixture failed ({}): stdout={} stderr={}",
            output.status,
            String::from_utf8_lossy(&output.stdout),
            String::from_utf8_lossy(&output.stderr)
        );
        std::fs::rename(&fixture, &path).unwrap();
        let app = crate::shortcuts::import_app(path.to_str().unwrap()).unwrap();
        assert_eq!(app.title, "测试应用 ' 中文");
        assert!(app.target.ends_with(".lnk"));
        assert!(!app.icon.is_empty());
        let exe = std::path::PathBuf::from(std::env::var_os("SystemRoot").unwrap())
            .join("System32/cmd.exe");
        assert_ne!(
            app.icon,
            metadata(&exe).1,
            "custom shortcut icon must not be replaced with the target icon"
        );
        crate::shortcuts::validate(&[app]).unwrap();
    }
}
