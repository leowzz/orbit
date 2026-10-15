//! Explicit, local favicon retrieval; no browser cookies or third-party icon service.
use base64::{engine::general_purpose::STANDARD, Engine};
use reqwest::Client;
use scraper::{Html, Selector};
use std::{io::Cursor, time::Duration};
use url::Url;

const MAX_BYTES: usize = 2 * 1024 * 1024;
const NOT_FOUND: &str = "未能获取网站图标，请重试或手动选择图片";

#[derive(Debug, serde::Serialize)]
pub struct WebsiteInfo {
    pub icon: String,
    pub title: String,
    pub warning: Option<String>,
}

pub async fn fetch(raw: &str) -> Result<WebsiteInfo, String> {
    if !crate::shortcuts::web_url(raw) {
        return Err("请先填写有效的 http/https 网址".into());
    }
    tokio::time::timeout(
        Duration::from_secs(20),
        fetch_inner(Url::parse(raw).unwrap()),
    )
    .await
    .map_err(|_| "获取图标超时，请重试或手动选择图片".to_string())?
}

async fn fetch_inner(mut page: Url) -> Result<WebsiteInfo, String> {
    page.set_fragment(None);
    let client = Client::builder()
        .user_agent("Orbit-Agent/0.1")
        .timeout(Duration::from_secs(5))
        .redirect(reqwest::redirect::Policy::custom(|attempt| {
            if attempt.previous().len() >= 5 || !crate::shortcuts::web_url(attempt.url().as_str()) {
                attempt.error("invalid icon redirect")
            } else {
                attempt.follow()
            }
        }))
        .build()
        .map_err(|_| NOT_FOUND)?;
    let mut candidates = Vec::new();
    let mut title = String::new();
    if let Ok((final_url, bytes)) = download(&client, page.clone()).await {
        page = final_url;
        let source = String::from_utf8_lossy(&bytes);
        candidates = icon_links(&page, &source);
        title = website_title(&source);
    }
    let fallback = page.join("/favicon.ico").map_err(|_| NOT_FOUND)?;
    if !candidates.contains(&fallback) {
        candidates.push(fallback);
    }
    for candidate in candidates {
        if let Ok((_, bytes)) = download(&client, candidate).await {
            if let Ok(Ok(icon)) = tokio::task::spawn_blocking(move || normalize(&bytes)).await {
                return Ok(WebsiteInfo {
                    icon,
                    title,
                    warning: None,
                });
            }
        }
    }
    if !title.is_empty() {
        return Ok(WebsiteInfo {
            icon: String::new(),
            title,
            warning: Some(NOT_FOUND.into()),
        });
    }
    Err(NOT_FOUND.into())
}

fn website_title(source: &str) -> String {
    let html = Html::parse_document(source);
    for selector in [
        "meta[property='og:site_name']",
        "meta[name='application-name']",
        "title",
        "meta[property='og:title']",
    ] {
        for el in html.select(&Selector::parse(selector).unwrap()) {
            let raw = if el.value().name() == "title" {
                el.text().collect::<String>()
            } else {
                el.value().attr("content").unwrap_or_default().to_owned()
            };
            let normalized = raw.split_whitespace().collect::<Vec<_>>().join(" ");
            let mut title = String::new();
            for ch in normalized.chars().filter(|c| !c.is_control()).take(80) {
                if title.len() + ch.len_utf8() > 160 {
                    break;
                }
                title.push(ch);
            }
            if !title.trim().is_empty() {
                return title.trim().to_owned();
            }
        }
    }
    String::new()
}

async fn download(client: &Client, url: Url) -> Result<(Url, Vec<u8>), String> {
    let mut response = client.get(url).send().await.map_err(|_| NOT_FOUND)?;
    if !response.status().is_success()
        || response
            .content_length()
            .is_some_and(|n| n > MAX_BYTES as u64)
    {
        return Err(NOT_FOUND.into());
    }
    let final_url = response.url().clone();
    let mut bytes = Vec::new();
    while let Some(chunk) = response.chunk().await.map_err(|_| NOT_FOUND)? {
        if bytes.len() + chunk.len() > MAX_BYTES {
            return Err(NOT_FOUND.into());
        }
        bytes.extend_from_slice(&chunk);
    }
    Ok((final_url, bytes))
}

fn icon_links(page: &Url, source: &str) -> Vec<Url> {
    let html = Html::parse_document(source);
    let base = html
        .select(&Selector::parse("base[href]").unwrap())
        .next()
        .and_then(|el| page.join(el.value().attr("href")?).ok())
        .filter(|u| crate::shortcuts::web_url(u.as_str()))
        .unwrap_or_else(|| page.clone());
    let mut result = Vec::new();
    for el in html.select(&Selector::parse("link[rel][href]").unwrap()) {
        let rel = el.value().attr("rel").unwrap();
        if !rel.split_ascii_whitespace().any(|r| {
            r.eq_ignore_ascii_case("icon")
                || r.eq_ignore_ascii_case("apple-touch-icon")
                || r.eq_ignore_ascii_case("apple-touch-icon-precomposed")
        }) {
            continue;
        }
        if let Ok(mut url) = base.join(el.value().attr("href").unwrap()) {
            url.set_fragment(None);
            if crate::shortcuts::web_url(url.as_str()) && !result.contains(&url) {
                result.push(url);
            }
        }
        if result.len() >= 8 {
            break;
        }
    }
    result
}

fn normalize(bytes: &[u8]) -> Result<String, String> {
    let png = if let Ok(format) = image::guess_format(bytes) {
        let mut reader = image::ImageReader::with_format(Cursor::new(bytes), format);
        let mut limits = image::Limits::default();
        limits.max_image_width = Some(4096);
        limits.max_image_height = Some(4096);
        limits.max_alloc = Some(64 * 1024 * 1024);
        reader.limits(limits);
        let decoded = reader.decode().map_err(|_| NOT_FOUND)?.thumbnail(64, 64);
        let mut out = Cursor::new(Vec::new());
        decoded
            .write_to(&mut out, image::ImageFormat::Png)
            .map_err(|_| NOT_FOUND)?;
        out.into_inner()
    } else {
        // SVG images must never resolve local files or external resources.
        let options = resvg::usvg::Options {
            image_href_resolver: resvg::usvg::ImageHrefResolver {
                resolve_data: Box::new(|_, _, _| None),
                resolve_string: Box::new(|_, _| None),
            },
            ..Default::default()
        };
        let source = std::str::from_utf8(bytes).map_err(|_| NOT_FOUND)?;
        let tree = resvg::usvg::Tree::from_str(source, &options).map_err(|_| NOT_FOUND)?;
        let scale = 64.0 / tree.size().width().max(tree.size().height());
        let mut pixmap = resvg::tiny_skia::Pixmap::new(64, 64).ok_or(NOT_FOUND)?;
        let transform = resvg::tiny_skia::Transform::from_scale(scale, scale).post_translate(
            (64.0 - tree.size().width() * scale) / 2.0,
            (64.0 - tree.size().height() * scale) / 2.0,
        );
        resvg::render(&tree, transform, &mut pixmap.as_mut());
        pixmap.encode_png().map_err(|_| NOT_FOUND)?
    };
    if png.len() > 32768 {
        return Err(NOT_FOUND.into());
    }
    Ok(STANDARD.encode(png))
}

#[cfg(test)]
mod tests {
    use super::*;
    const SVG: &str = r#"<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><rect width="32" height="32" fill="red"/></svg>"#;

    #[test]
    fn resolves_html_links_and_rejects_non_web_resources() {
        let page = Url::parse("https://example.com/path/page").unwrap();
        let links = icon_links(
            &page,
            r#"<base href="/assets/"><link href='logo.png?a=1&amp;b=2' rel='shortcut ICON'><link rel="icon" href="logo.png?a=1&amp;b=2"><link rel="icon" href="file:///private/icon.png"><link rel="icon" href="https://user:secret@other.test/icon"><link rel="apple-touch-icon" href="//cdn.example.com/apple.png">"#,
        );
        assert_eq!(
            links.iter().map(Url::as_str).collect::<Vec<_>>(),
            vec![
                "https://example.com/assets/logo.png?a=1&b=2",
                "https://cdn.example.com/apple.png"
            ]
        );
    }

    #[test]
    fn extracts_site_name_with_title_fallback_and_config_limits() {
        assert_eq!(website_title("<title>Page title</title><meta property='og:site_name' content='  网站 &amp; 名称  '>"), "网站 & 名称");
        assert_eq!(
            website_title("<title>  Example\n  Site </title>"),
            "Example Site"
        );
        assert_eq!(
            website_title("<title> </title><meta property='og:title' content='Fallback'>"),
            "Fallback"
        );
        let title = website_title(&format!("<title>{}</title>", "网".repeat(100)));
        assert!(title.len() <= 160);
        assert!(!title.is_empty());
        assert!(website_title("<html></html>").is_empty());
    }

    #[test]
    fn normalizes_raster_and_svg_and_rejects_invalid_images() {
        let svg = STANDARD.decode(normalize(SVG.as_bytes()).unwrap()).unwrap();
        let img = image::load_from_memory(&svg).unwrap();
        assert_eq!((img.width(), img.height()), (64, 64));
        let mut ico = Cursor::new(Vec::new());
        img.write_to(&mut ico, image::ImageFormat::Ico).unwrap();
        assert!(normalize(ico.get_ref()).is_ok());
        assert!(normalize(b"<html>not an icon</html>").is_err());
    }

    #[tokio::test]
    async fn follows_page_redirect_and_falls_back_after_invalid_link() {
        use tokio::io::{AsyncReadExt, AsyncWriteExt};
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let server = tokio::spawn(async move {
            loop {
                let (mut stream, _) = listener.accept().await.unwrap();
                let mut req = [0; 4096];
                let n = stream.read(&mut req).await.unwrap();
                let request = String::from_utf8_lossy(&req[..n]);
                let (status, extra, body) = if request.starts_with("GET /start ") {
                    ("302 Found", "Location: /nested/page\r\n", "")
                } else if request.starts_with("GET /nested/page ") {
                    (
                        "200 OK",
                        "",
                        "<title>Test Site</title><link rel='icon' href='broken.png'>",
                    )
                } else if request.starts_with("GET /favicon.ico ") {
                    ("200 OK", "", SVG)
                } else {
                    ("200 OK", "", "not an image")
                };
                let response = format!("HTTP/1.1 {status}\r\n{extra}Content-Length: {}\r\nConnection: close\r\n\r\n{body}", body.len());
                stream.write_all(response.as_bytes()).await.unwrap();
            }
        });
        let result = fetch(&format!("http://{addr}/start")).await;
        server.abort();
        assert_eq!(result.unwrap().title, "Test Site");
        assert!(fetch("file:///etc/hosts").await.is_err());
        assert!(fetch("https://user:secret@example.com").await.is_err());
    }
}
