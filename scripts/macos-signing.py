#!/usr/bin/env python3
"""Import a pinned signing identity on disposable GitHub runners; verify artifacts."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import platform
import secrets
import shlex
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parent.parent
CERTIFICATE = ROOT / "agents/desktop/signing/macos-agent.cer"
SECRET_NAMES = ("APPLE_CERTIFICATE", "APPLE_CERTIFICATE_PASSWORD", "APPLE_SIGNING_IDENTITY")


def command_name(args):
    command = args[2:] if args[:2] == ["sudo", "-n"] else args
    return f"{Path(command[0]).name} {command[1]}"


def run(args, timeout=60, **kwargs):
    try:
        result = subprocess.run(args, capture_output=True, check=False, timeout=timeout, **kwargs)
    except subprocess.TimeoutExpired:
        # TimeoutExpired includes the full command, which can contain passwords.
        raise RuntimeError(f"Signing command timed out after {timeout}s: {command_name(args)}") from None
    if result.returncode:
        # Never echo command arguments, environment or security's key attributes.
        raise RuntimeError(f"Signing command failed: {command_name(args)}")
    return result.stdout


def work_directory():
    return Path(os.environ["RUNNER_TEMP"]) / "orbit-macos-signing"


def require_disposable_runner():
    if (platform.system() != "Darwin" or os.environ.get("GITHUB_ACTIONS") != "true"
            or os.environ.get("ORBIT_SIGNING_RUNNER_ENVIRONMENT") != "github-hosted"):
        raise RuntimeError("Signing setup/cleanup requires a disposable GitHub-hosted macOS runner")


def cleanup():
    require_disposable_runner()
    work = work_directory()
    state_file = work / "state.json"
    if not state_file.exists():
        return
    state = json.loads(state_file.read_text())
    failures = []
    # Remove private signing material before touching the system certificate store.
    (work / "certificate.p12").unlink(missing_ok=True)
    commands = [["security", "list-keychains", "-d", "user", "-s", *state["keychains"]]]
    if state.get("keychain_created"):
        commands.append(["security", "delete-keychain", str(work / "signing.keychain-db")])
    if state.get("trusted"):
        # remove-trusted-cert can hang on headless macOS runners (runner-images#12116).
        # Delete the public certificate directly; any remaining trust database entry
        # disappears with the disposable VM. Never use this cleanup on a local host.
        commands.append(["sudo", "-n", "security", "delete-certificate", "-Z", state["identity"],
                         "/Library/Keychains/System.keychain"])
    for command in commands:
        print(f"macOS signing cleanup: {command_name(command)}", flush=True)
        try:
            run(command, timeout=30)
        except RuntimeError as error:
            failures.append(str(error))
    if failures:
        raise RuntimeError("; ".join(failures))
    shutil.rmtree(work)


def setup():
    require_disposable_runner()
    for name in SECRET_NAMES:
        if not os.environ.get(name):
            raise RuntimeError(f"Missing repository secret: {name}")
    # Pin the identity by certificate, rather than trusting an arbitrary P12 secret.
    identity = hashlib.sha1(CERTIFICATE.read_bytes()).hexdigest().upper()
    if os.environ["APPLE_SIGNING_IDENTITY"] != identity:
        raise RuntimeError("APPLE_SIGNING_IDENTITY does not match the pinned certificate")
    work = work_directory()
    work.mkdir(mode=0o700)
    os.umask(0o077)
    state = {"identity": identity, "keychains": shlex.split(run(
        ["security", "list-keychains", "-d", "user"]).decode())}
    state_file = work / "state.json"

    def save_state():
        state_file.write_text(json.dumps(state))

    save_state()
    try:
        p12 = work / "certificate.p12"
        p12.write_bytes(base64.b64decode(os.environ["APPLE_CERTIFICATE"], validate=True))
        pem = run(["openssl", "pkcs12", "-in", str(p12), "-clcerts", "-nokeys",
                   "-passin", "env:APPLE_CERTIFICATE_PASSWORD"])
        leaf = run(["openssl", "x509", "-outform", "DER"], input=pem)
        if leaf != CERTIFICATE.read_bytes():
            raise RuntimeError("P12 certificate does not match the pinned certificate")
        password = secrets.token_urlsafe(32)
        keychain = str(work / "signing.keychain-db")
        run(["security", "create-keychain", "-p", password, keychain])
        state["keychain_created"] = True
        save_state()
        run(["security", "set-keychain-settings", "-lut", "21600", keychain])
        run(["security", "unlock-keychain", "-p", password, keychain])
        run(["security", "import", str(p12), "-k", keychain, "-P",
             os.environ["APPLE_CERTIFICATE_PASSWORD"], "-T", "/usr/bin/codesign", "-T", "/usr/bin/security"])
        run(["security", "set-key-partition-list", "-S", "apple-tool:,apple:,codesign:",
             "-s", "-k", password, keychain])
        run(["security", "list-keychains", "-d", "user", "-s", keychain, *state["keychains"]])
        # Apple codesign needs a trusted identity to sign. This trust change is
        # restricted to disposable hosted runners; cleanup deletes the certificate.
        state["trusted"] = True
        save_state()
        run(["sudo", "-n", "security", "add-trusted-cert", "-d", "-r", "trustRoot",
             "-p", "codeSign", "-k", "/Library/Keychains/System.keychain", str(CERTIFICATE)])
        identities = run(["security", "find-identity", "-v", "-p", "codesigning", keychain]).decode()
        if identity not in identities:
            raise RuntimeError("Imported certificate is not a valid code-signing identity")
        with open(os.environ["GITHUB_ENV"], "a") as environment:
            environment.write(f"APPLE_SIGNING_IDENTITY={identity}\n")
        p12.unlink()
        print("Imported the pinned Orbit signing identity into a temporary keychain")
    except Exception:
        cleanup()
        raise


def verify(artifact):
    artifact = Path(artifact).resolve()
    run(["/usr/bin/codesign", "--verify", "--deep", "--strict", str(artifact)])
    with tempfile.TemporaryDirectory(prefix="orbit-cert-check-") as temporary:
        prefix = str(Path(temporary) / "certificate-")
        run(["/usr/bin/codesign", "--display", f"--extract-certificates={prefix}", str(artifact)])
        leaf = Path(prefix + "0")
        if not leaf.is_file() or leaf.read_bytes() != CERTIFICATE.read_bytes():
            raise RuntimeError("Artifact is not signed with the pinned Orbit certificate")
    print(f"Verified Orbit certificate and code integrity: {artifact.name}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["setup", "cleanup", "verify"])
    parser.add_argument("artifact", nargs="?")
    arguments = parser.parse_args()
    try:
        if arguments.action == "verify":
            if not arguments.artifact:
                parser.error("verify requires an artifact path")
            verify(arguments.artifact)
        elif arguments.action == "setup":
            setup()
        else:
            cleanup()
    except Exception as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
