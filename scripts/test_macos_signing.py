"""Exercise credential guards, temporary keychain cleanup, and certificate pinning."""
import base64
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("macos_signing", Path(__file__).with_name("macos-signing.py"))
signing = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(signing)


class MacSigningTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.identity = hashlib.sha1(signing.CERTIFICATE.read_bytes()).hexdigest().upper()
        self.environment = {
            "RUNNER_TEMP": str(self.root), "GITHUB_ACTIONS": "true",
            "ORBIT_SIGNING_RUNNER_ENVIRONMENT": "github-hosted",
            "GITHUB_ENV": str(self.root / "environment"),
            "APPLE_CERTIFICATE": base64.b64encode(b"fake test P12").decode(),
            "APPLE_CERTIFICATE_PASSWORD": "test-password",
            "APPLE_SIGNING_IDENTITY": self.identity,
        }
        self.addCleanup(patch.stopall)
        patch.dict(os.environ, self.environment, clear=True).start()
        patch.object(signing.platform, "system", return_value="Darwin").start()
        # setup's umask change is intended for the worker, not the test process.
        previous_umask = os.umask(0o077)
        self.addCleanup(os.umask, previous_umask)
        self.calls = []

    def run_command(self, args, **kwargs):
        self.calls.append(args)
        if args[:2] == ["security", "list-keychains"] and "-s" not in args:
            return b'"/original/login.keychain-db"\n'
        if args[:2] == ["openssl", "pkcs12"]:
            return b"public PEM fixture"
        if args[:2] == ["openssl", "x509"]:
            return signing.CERTIFICATE.read_bytes()
        if args[:2] == ["security", "find-identity"]:
            return self.identity.encode()
        return b""

    def test_rejects_local_or_self_hosted_setup_without_mutation(self):
        for environment in ["self-hosted", ""]:
            with self.subTest(environment=environment), patch.dict(os.environ, {"ORBIT_SIGNING_RUNNER_ENVIRONMENT": environment}), patch.object(signing, "run") as command:
                with self.assertRaisesRegex(RuntimeError, "disposable"):
                    signing.setup()
                command.assert_not_called()
                self.assertFalse(signing.work_directory().exists())

    def test_missing_secret_or_wrong_identity_never_imports(self):
        for name, value in [("APPLE_CERTIFICATE", ""), ("APPLE_SIGNING_IDENTITY", "wrong")]:
            with self.subTest(name=name), patch.dict(os.environ, {name: value}), patch.object(signing, "run") as command:
                with self.assertRaises(RuntimeError):
                    signing.setup()
                command.assert_not_called()
                self.assertFalse(signing.work_directory().exists())

    def test_setup_and_cleanup_restore_keychains_and_remove_trust(self):
        with patch.object(signing, "run", side_effect=self.run_command):
            signing.setup()
            work = signing.work_directory()
            state = json.loads((work / "state.json").read_text())
            self.assertTrue(state["trusted"])
            self.assertFalse((work / "certificate.p12").exists())
            self.assertEqual((self.root / "environment").read_text(), f"APPLE_SIGNING_IDENTITY={self.identity}\n")
            signing.cleanup()
        self.assertFalse(work.exists())
        self.assertIn(["security", "list-keychains", "-d", "user", "-s", "/original/login.keychain-db"], self.calls)
        self.assertTrue(any(command[2:4] == ["security", "remove-trusted-cert"] for command in self.calls))
        self.assertIn(["security", "delete-keychain", str(work / "signing.keychain-db")], self.calls)

    def test_failed_setup_after_trust_runs_cleanup(self):
        def fail_identity(args, **kwargs):
            if args[:2] == ["security", "find-identity"]:
                return b"0 valid identities"
            return self.run_command(args, **kwargs)
        with patch.object(signing, "run", side_effect=fail_identity):
            with self.assertRaisesRegex(RuntimeError, "not a valid"):
                signing.setup()
        self.assertFalse(signing.work_directory().exists())
        self.assertTrue(any(command[2:4] == ["security", "remove-trusted-cert"] for command in self.calls))

    def test_wrong_p12_certificate_is_rejected_before_import(self):
        def wrong_leaf(args, **kwargs):
            if args[:2] == ["openssl", "x509"]:
                return b"another certificate"
            return self.run_command(args, **kwargs)
        with patch.object(signing, "run", side_effect=wrong_leaf):
            with self.assertRaisesRegex(RuntimeError, "does not match"):
                signing.setup()
        self.assertFalse(signing.work_directory().exists())
        self.assertFalse(any(command[:2] == ["security", "import"] for command in self.calls))

    def test_verify_rejects_ad_hoc_or_other_certificate(self):
        for certificate in [None, b"another certificate", signing.CERTIFICATE.read_bytes()]:
            def extract(args, **kwargs):
                for argument in args:
                    if argument.startswith("--extract-certificates=") and certificate is not None:
                        Path(argument.split("=", 1)[1] + "0").write_bytes(certificate)
                return b""
            with self.subTest(certificate=certificate is not None), patch.object(signing, "run", side_effect=extract):
                if certificate == signing.CERTIFICATE.read_bytes():
                    signing.verify(self.root / "app")
                else:
                    with self.assertRaisesRegex(RuntimeError, "pinned"):
                        signing.verify(self.root / "app")


if __name__ == "__main__":
    unittest.main()
