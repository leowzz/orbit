# Tagged releases

Pushing a `v*` tag runs `.github/workflows/release.yml`. Use Docker-compatible
semantic version tags such as `v1.2.3` or `v1.2.3-rc.1` (no build metadata,
at most 128 characters). A tag containing a hyphen becomes a prerelease.
`make release V=v1.2.3` creates a local commit/tag; it does not push them.

Pushing `main` runs `.github/workflows/unit-tests.yml`, covering Go/Web, scripts,
Android, the desktop Agent, and ESP32-S3 config/native tests without packaging,
signing, or uploading distributable artifacts.

`Windows Agent` (`.github/workflows/windows-agent.yml`) additionally runs on branch
pushes and pull requests touching the desktop Agent, canonical protocol, or its
build workflows, and supports manual dispatch. It checks TypeScript, UI tests,
Rust formatting/clippy/tests on `windows-2022`, builds a release-mode x64 NSIS
installer, and uploads `windows-agent-x64` (installer plus `WINDOWS-SHA256SUMS`,
retained for 14 days). Download it from the workflow run's Artifacts section.
The tag release calls this same workflow with the tag version and includes its
`release-windows-agent` artifact in the final Release. No tag is needed to test
Windows packaging. Rust compilation uses two jobs to limit linker memory use.

## Outputs

- Linux amd64 images: `ghcr.io/leowzz/orbit-agent:TAG`,
  `ghcr.io/leowzz/orbit-core:TAG`, `ghcr.io/leowzz/orbit-web:TAG`.
  The workflow derives the lowercase owner from the repository. Each image is
  signed by digest using GitHub OIDC and Cosign. Existing Aliyun mirror publishing
  and signing runs when both Aliyun registry secrets are present.
- `orbit-android-arm64-v8a-TAG.apk`, `orbit-android-armeabi-v7a-TAG.apk`, and
  `orbit-android-x86_64-TAG.apk`: release APKs containing only one CPU architecture,
  signed with the persistent Android key. Most current phones use `arm64-v8a`;
  `armeabi-v7a` supports 32-bit ARM devices and `x86_64` supports Intel/AMD devices
  and emulators. The updater chooses the device's preferred supported architecture.
  `orbit-android-TAG.apk` remains available for older updaters that only recognize
  the universal package name. The version name is the tag without `v`; the base
  version code is the workflow run number, with Flutter's ABI offsets on split
  APKs. Keep the workflow and key stable for upgrades.
- `orbit-agent-macos-universal-TAG.dmg`: the Tauri desktop Agent for Apple Silicon
  and Intel, requiring macOS 12 or later. The app version is injected from the tag
  without `v`. CI verifies both binary architectures, the app version and the DMG.
  The app and DMG are signed with the persistent Orbit self-signed certificate.
  CI verifies code integrity and the exact pinned signing certificate. Apple Developer ID
  signing and notarization are not configured;
  first launch may require manual approval in System Settings → Privacy & Security.
  Cosign verifies the download but does not replace Apple signing or notarization.
- `orbit-agent-windows-x64-TAG-setup.exe`: Windows x64 NSIS installer, with
  embedded frontend assets and WebView2 bootstrapper support. The version is
  injected from the tag. Authenticode signing is not configured; the Release
  supplies SHA-256 checksums and Cosign download verification. ASR GGUF models
  and speech.cpp/Opus DLLs are not bundled; configure them in the Agent.
- `orbit-esp32s3-yd-oled-128x32-TAG.tar.gz`: application, bootloader, partitions,
  boot_app0, merged factory image and flash instructions for YD-ESP32-S3 / OLED
  128x32. Firmware reports the tag version. It embeds example network placeholders;
  rebuild with private `config.local.yaml` to connect to your Wi-Fi/MQTT broker.
- `SHA256SUMS` and a `.sigstore.json` Cosign bundle for each download/checksum file.
  These are artifact signatures, not ESP Secure Boot signatures or eFuse changes.

Go tests, desktop Agent TypeScript/UI/Rust checks and tests, Flutter analysis/tests
and ESP32-S3 config/native tests run before their builds. The final job requires
all builds and image signatures to succeed, uploads
files to a draft, then publishes the Release with GHCR tags and immutable digests.
A published Release cannot be overwritten by rerunning the final job.

The desktop test and release jobs share a Rust dependency cache. A new tag can
restore the cache populated on `main`; caches created under one tag are not
available to other tags. Universal release compilation still builds both CPU
architectures when their release dependencies are not cached.

macOS signing commands have a 60-second timeout (30 seconds during cleanup).
Cleanup removes the temporary private keychain before deleting the public system
certificate by fingerprint. It avoids `security remove-trusted-cert`, which can
hang on headless runners; any remaining trust entry is discarded with the hosted
VM. Cleanup is restricted to disposable GitHub-hosted runners and has a three-minute
step timeout. It never runs on developer machines or self-hosted runners.

## In-app updates

Mac Agent's **偏好设置 → 软件更新** and Android's **设置 → 软件更新** check the
latest stable GitHub Release on startup (when the update UI is loaded) and every
six hours while running. Manual checks are also available. Drafts, prereleases
and versions older than or equal to the installed version are never installed.
No extra update signing secret or update feed is required: downloads must match
the exact repository/tag asset URL, size and SHA-256 digest returned by GitHub's
HTTPS API. This trusts the GitHub repository and TLS; the in-app updater does not
verify the separately published Cosign bundles.

The Mac updater uses the existing universal DMG, verifies its app identifier,
version, code signature and exact Orbit certificate, stages it alongside the current app, exits, replaces
the app and reopens it. Settings and Keychain credentials are preserved. Install
the Agent in a writable application directory first; apps opened from a mounted
DMG or App Translocation cannot update in place. Failed replacement or launch
attempts restore the previous bundle; updater logs remain in the app cache on
failure. Unsaved settings must be saved or discarded before updating.

Android verifies the APK's package name, version code/name and signing certificate
before opening the system installer through a read-only FileProvider URI. The
first update may require allowing Orbit to install apps; returning after granting
that permission continues to the installer. The user must confirm installation
in Android. Cancelling permits retry without downloading again. Debug-key builds
cannot be upgraded with the differently signed release APK; benchmark builds
never offer installation of the production package. Keep the release keystore
and increasing workflow run numbers stable to preserve upgrade compatibility.
New clients prefer an APK matching `Build.SUPPORTED_ABIS`, in device preference
order, and fall back to the universal APK for older releases.

## Repository secrets

Configure these under GitHub **Settings → Secrets and variables → Actions**:

| Secret | Value |
| --- | --- |
| `ANDROID_KEYSTORE_BASE64` | Base64-encoded persistent release keystore |
| `ANDROID_KEYSTORE_PASSWORD` | Keystore password |
| `ANDROID_KEY_ALIAS` | Signing key alias |
| `ANDROID_KEY_PASSWORD` | Signing key password |
| `APPLE_CERTIFICATE` | Base64-encoded Orbit self-signed code-signing P12 |
| `APPLE_CERTIFICATE_PASSWORD` | P12 export password |
| `APPLE_SIGNING_IDENTITY` | SHA-1 identity of the pinned Orbit signing certificate |
| `ALIYUN_REGISTRY_USERNAME` | Existing optional mirror login |
| `ALIYUN_REGISTRY_PASSWORD` | Existing optional mirror password |

Back up the Android keystore and passwords securely and reuse them for future
releases. Existing app installations require the same signing key for upgrades.
CI fails before publishing images when Android or Mac signing secrets are absent.
GHCR and Cosign use the job's `GITHUB_TOKEN` / OIDC; no Cosign private-key secret
is required. Repository/package policy must allow the workflow to write packages.

For a local signed APK, export `ANDROID_KEYSTORE_PATH` (absolute path),
`ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, and `ANDROID_KEY_PASSWORD`, then
run `flutter build apk --release --split-per-abi` from `nodes/app`. Outputs are
`build/app/outputs/flutter-apk/app-<abi>-release.apk`. Release builds fail without
these values; debug builds need no release credentials.

### Mac signing identity

The private signing material is stored outside the repository under
`~/.config/orbit/signing/`: `macos-agent-key.pem` (encrypted RSA-3072 private key),
`macos-agent-key-password.txt`, `macos-agent-certificate.pem`,
`macos-agent-certificate.cer`, `macos-agent-certificate.p12` and
`macos-agent-p12-password.txt`. The directory is mode `0700`; private files are
mode `0600`. Reuse and securely back up this identity for subsequent releases.
Only the public DER certificate, `agents/desktop/signing/macos-agent.cer`, is in
the repository and embedded in the updater. Its SHA-256 fingerprint is
`D5A6CCDABF5B43A8808076D5FA3C655622C09104314249D57B4EBE61FD0A222E`.
Changing that pinned certificate requires a client migration; do not rotate it
as part of an ordinary release.

Both trusted main/manual desktop builds and tag releases run
`scripts/macos-signing.py setup` after tests, sign through Tauri, and verify the
result with `scripts/macos-signing.py verify <artifact>`. Missing credentials
fail packaging; there is no ad-hoc fallback. Pull requests run checks/tests
without importing signing credentials or uploading a package.

Setup is restricted to disposable GitHub-hosted macOS runners. It imports the
P12 into a temporary keychain, grants Apple's signing tools access, and trusts
the pinned self-signed certificate for code signing on that runner. An
`always()` step removes that trust, deletes the certificate and keychain, and
restores the runner's original keychain search list, including failure paths.
This workflow does not install trust into a developer's or user's Mac.
Local certificate-backed Tauri packaging requires the same identity to be
available in a code-signing keychain; `tauri dev` does not package/sign an app.

## Verify downloads and images

Using Cosign 2.5.2 or compatible, substitute the exact published tag:

```sh
TAG=v1.2.3
IDENTITY="https://github.com/leowzz/orbit/.github/workflows/release.yml@refs/tags/$TAG"
cosign verify --certificate-identity "$IDENTITY" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  "ghcr.io/leowzz/orbit-core:$TAG"
cosign verify-blob --bundle SHA256SUMS.sigstore.json \
  --certificate-identity "$IDENTITY" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com SHA256SUMS
sha256sum --check SHA256SUMS
```

Download the APK, macOS DMG and firmware archive to check the complete checksum list.
You can also verify an individual download with its matching Cosign bundle.
See [Sigstore CI signing](https://docs.sigstore.dev/quickstart/quickstart-ci/),
[Flutter Android signing](https://docs.flutter.dev/deployment/android) and
[Tauri macOS distribution](https://v2.tauri.app/distribute/macos-application-bundle/).
