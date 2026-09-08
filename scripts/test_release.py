"""Exercise local release commits/tags in disposable repositories."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


class ReleaseTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="orbit-release-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / "scripts").mkdir()
        shutil.copy(Path(__file__).with_name("release.sh"), self.root / "scripts/release.sh")
        self.local = self.root / ".env"
        self.tracked = self.root / ".env.example"
        self.local.write_text("# local settings\nversion=v0.1.8\n\nTOKEN=$(touch should-not-exist)\n")
        self.tracked.write_text("# defaults\nversion=v0.1.8\nOTHER=example\n")
        (self.root / ".gitignore").write_text(".env\n")
        self.git("init", "-q")
        self.git("config", "user.name", "Release Test")
        self.git("config", "user.email", "release@example.invalid")
        self.git("config", "commit.gpgsign", "false")
        self.git("config", "tag.gpgsign", "false")
        self.git("add", ".")
        self.git("commit", "-qm", "initial")

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.root, text=True).strip()

    def release(self, version=""):
        return subprocess.run(
            ["bash", "scripts/release.sh"], cwd=self.root,
            env=os.environ | {"V": version, "ENV_FILE": ".env"},
            capture_output=True, text=True,
        )

    def assert_success(self, version="", expected="v0.1.9"):
        before_local = self.local.read_text()
        before_tracked = self.tracked.read_text()
        result = self.release(version)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.local.read_text(), before_local.replace("v0.1.8", expected))
        self.assertEqual(self.tracked.read_text(), before_tracked.replace("v0.1.8", expected))
        self.assertEqual(self.git("cat-file", "-t", expected), "tag")
        self.assertEqual(self.git("log", "-1", "--format=%s"), f"chore: release {expected}")
        self.assertEqual(self.git("status", "--porcelain"), "")
        self.assertFalse((self.root / "should-not-exist").exists())

    def assert_rejected(self, version=""):
        before = (self.local.read_bytes(), self.tracked.read_bytes(), self.git("rev-parse", "HEAD"), self.git("tag"))
        result = self.release(version)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(before, (self.local.read_bytes(), self.tracked.read_bytes(), self.git("rev-parse", "HEAD"), self.git("tag")))

    def test_default_bumps_patch_and_preserves_settings(self):
        self.assert_success()

    def test_explicit_version(self):
        self.assert_success("v1.2.3", "v1.2.3")

    def test_single_line_env_still_supported(self):
        self.local.write_text("version=v0.1.8\n")
        self.assert_success()

    def test_invalid_local_versions_leave_repository_unchanged(self):
        for content in ["TOKEN=example\n", "version=v0.1.8\nversion=v0.1.9\n", "version=v0.1.bad\n"]:
            with self.subTest(content=content):
                self.local.write_text(content)
                self.assert_rejected()

    def test_invalid_tracked_version_leaves_local_unchanged(self):
        self.tracked.write_text("OTHER=example\n")
        self.git("add", ".env.example")
        self.git("commit", "-qm", "invalid template")
        self.assert_rejected()

    def test_invalid_explicit_version(self):
        self.assert_rejected("v1.2.bad")

    def test_existing_tag(self):
        self.git("tag", "v0.1.9")
        self.assert_rejected()

    def test_dirty_worktree(self):
        (self.root / "unrelated.txt").write_text("uncommitted work")
        self.assert_rejected()


if __name__ == "__main__":
    unittest.main()
