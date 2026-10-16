"""Exercise credential guards, temporary keychain cleanup, and certificate pinning."""
import base64
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
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

    def test_setup_and_cleanup_remove_private_keychain_before_system_certificate(self):
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
        private_keychain = ["security", "delete-keychain", str(work / "signing.keychain-db")]
        system_certificate = ["sudo", "-n", "security", "delete-certificate", "-Z", self.identity,
                              "/Library/Keychains/System.keychain"]
        self.assertLess(self.calls.index(private_keychain), self.calls.index(system_certificate))
        self.assertFalse(any("remove-trusted-cert" in command for command in self.calls))

    def test_failed_setup_after_trust_runs_cleanup(self):
        def fail_identity(args, **kwargs):
            if args[:2] == ["security", "find-identity"]:
                return b"0 valid identities"
            return self.run_command(args, **kwargs)
        with patch.object(signing, "run", side_effect=fail_identity):
            with self.assertRaisesRegex(RuntimeError, "not a valid"):
                signing.setup()
        self.assertFalse(signing.work_directory().exists())
        self.assertTrue(any(command[2:4] == ["security", "delete-certificate"] for command in self.calls))

    def test_cleanup_timeout_still_removes_private_material_and_reports_command(self):
        with patch.object(signing, "run", side_effect=self.run_command):
            signing.setup()
        work = signing.work_directory()
        p12 = work / "certificate.p12"
        p12.write_bytes(b"unfinished import")
        self.calls.clear()

        def fail_restore(args, **kwargs):
            self.assertEqual(kwargs["timeout"], 30)
            self.calls.append(args)
            if args[1] == "list-keychains":
                raise RuntimeError("Signing command timed out after 30s: security list-keychains")

        with patch.object(signing, "run", side_effect=fail_restore):
            with self.assertRaisesRegex(RuntimeError, "security list-keychains"):
                signing.cleanup()
        self.assertFalse(p12.exists())
        self.assertIn(["security", "delete-keychain", str(work / "signing.keychain-db")], self.calls)
        self.assertTrue(any("delete-certificate" in command for command in self.calls))

    def test_command_timeout_does_not_expose_passwords(self):
        args = ["security", "create-keychain", "-p", "secret-password", "keychain"]
        with patch.object(signing.subprocess, "run", side_effect=subprocess.TimeoutExpired(args, 60)) as command:
            with self.assertRaisesRegex(RuntimeError, "timed out after 60s: security create-keychain") as error:
                signing.run(args)
        self.assertNotIn("secret-password", str(error.exception))
        self.assertEqual(command.call_args.kwargs["timeout"], 60)

    def test_hung_command_is_terminated(self):
        with self.assertRaisesRegex(RuntimeError, "timed out"):
            signing.run([sys.executable, "-c", "import time; time.sleep(30)"], timeout=0.1)

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
