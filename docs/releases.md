# Tagged releases

Pushing a `v*` tag runs `.github/workflows/release.yml`. Use Docker-compatible
tags such as `v1.2.3` or `v1.2.3-rc.1` (letters, digits, dots, underscores and
hyphens, at most 128 characters). A tag containing a hyphen becomes a prerelease.
`make release V=v1.2.3` creates a local commit/tag; it does not push them.

## Outputs

- Linux amd64 images: `ghcr.io/leowzz/orbit-agent:TAG`,
  `ghcr.io/leowzz/orbit-core:TAG`, `ghcr.io/leowzz/orbit-web:TAG`.
  The workflow derives the lowercase owner from the repository. Each image is
  signed by digest using GitHub OIDC and Cosign. Existing Aliyun mirror publishing
  and signing runs when both Aliyun registry secrets are present.
- `orbit-android-TAG.apk`: a release APK signed with the persistent Android key.
  The version name is the tag without `v`; the version code is the workflow run
  number. Keep the workflow and key stable for upgrades.
- `orbit-esp32s3-yd-oled-128x32-TAG.tar.gz`: application, bootloader, partitions,
  boot_app0, merged factory image and flash instructions for YD-ESP32-S3 / OLED
  128x32. Firmware reports the tag version. It embeds example network placeholders;
  rebuild with private `config.local.yaml` to connect to your Wi-Fi/MQTT broker.
- `SHA256SUMS` and a `.sigstore.json` Cosign bundle for each download/checksum file.
  These are artifact signatures, not ESP Secure Boot signatures or eFuse changes.

Go tests, Flutter analysis/tests and ESP32-S3 config/native tests run before their
builds. The final job requires all builds and image signatures to succeed, uploads
files to a draft, then publishes the Release with GHCR tags and immutable digests.
A published Release cannot be overwritten by rerunning the final job.

## Repository secrets

Configure these under GitHub **Settings → Secrets and variables → Actions**:

| Secret | Value |
| --- | --- |
| `ANDROID_KEYSTORE_BASE64` | Base64-encoded persistent release keystore |
| `ANDROID_KEYSTORE_PASSWORD` | Keystore password |
| `ANDROID_KEY_ALIAS` | Signing key alias |
| `ANDROID_KEY_PASSWORD` | Signing key password |
| `ALIYUN_REGISTRY_USERNAME` | Existing optional mirror login |
| `ALIYUN_REGISTRY_PASSWORD` | Existing optional mirror password |

Back up the Android keystore and passwords securely and reuse them for future
releases. Existing app installations require the same signing key for upgrades.
CI fails before publishing images when Android signing secrets are absent.
GHCR and Cosign use the job's `GITHUB_TOKEN` / OIDC; no Cosign private-key secret
is required. Repository/package policy must allow the workflow to write packages.

For a local signed APK, export `ANDROID_KEYSTORE_PATH` (absolute path),
`ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, and `ANDROID_KEY_PASSWORD`, then
run `flutter build apk --release` from `nodes/android`. Release builds fail without
these values; debug builds need no release credentials.

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

Download both the APK and firmware archive to check the complete checksum list.
You can also verify an individual download with its matching Cosign bundle.
See [Sigstore CI signing](https://docs.sigstore.dev/quickstart/quickstart-ci/) and
[Flutter Android signing](https://docs.flutter.dev/deployment/android).
