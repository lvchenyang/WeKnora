"""Regression tests using disposable local Git remotes; no GitHub access.

Run: python3 -m unittest discover -s dison -p 'test_*.py' -v
"""

import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("sync-local.sh").resolve()
GIT = shutil.which("git")


class SyncLocalTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="weknora-sync-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.env = os.environ.copy()
        for key in list(self.env):
            if key.startswith("GIT_"):
                del self.env[key]
        self.env.update({
            "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_CONFIG_SYSTEM": os.devnull,
            "GIT_AUTHOR_NAME": "Sync Test",
            "GIT_COMMITTER_NAME": "Sync Test",
            "GIT_AUTHOR_EMAIL": "sync@example.test",
            "GIT_COMMITTER_EMAIL": "sync@example.test",
            "GIT_TERMINAL_PROMPT": "0",
        })
        self.seed = self.root / "seed"
        self.seed.mkdir()
        self.git(self.seed, "init", "-b", "main")
        self.commit(self.seed, "base.txt", "base")
        self.upstream = self.root / "upstream.git"
        self.fork = self.root / "fork.git"
        self.git(self.root, "clone", "--bare", str(self.seed), str(self.upstream))
        self.git(self.root, "clone", "--bare", str(self.seed), str(self.fork))
        self.local = self.root / "local"
        self.git(self.root, "clone", str(self.upstream), str(self.local))
        self.git(self.local, "remote", "add", "fork", str(self.fork))
        self.git(self.local, "checkout", "-b", "dison/prod")
        self.git(self.local, "push", "fork", "dison/prod")
        self.writer = self.root / "writer"
        self.git(self.root, "clone", str(self.fork), str(self.writer))
        self.git(self.writer, "checkout", "dison/prod")
        (self.local / "dison").mkdir()
        shutil.copyfile(SCRIPT, self.local / "dison/sync-local.sh")

    def git(self, cwd, *args):
        result = subprocess.run(
            [GIT, *args], cwd=cwd, env=self.env, text=True,
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=30,
        )
        self.assertEqual(result.returncode, 0, result.stdout)
        return result.stdout.strip()

    def commit(self, cwd, name, content):
        (cwd / name).write_text(content + "\n")
        self.git(cwd, "add", name)
        self.git(cwd, "commit", "-m", content)
        return self.git(cwd, "rev-parse", "HEAD")

    def sync(self, env=None, shell="bash"):
        return subprocess.run(
            [shell, "dison/sync-local.sh"], cwd=self.local,
            env=env or self.env, text=True, stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT, timeout=30,
        )

    def advance_upstream(self):
        self.commit(self.seed, "upstream.txt", "upstream advance")
        self.git(self.seed, "push", str(self.upstream), "main")

    def assert_contains(self, repo, ref, filename, text):
        self.assertEqual(self.git(repo, "show", f"{ref}:{filename}"), text)

    def test_remote_ahead_stops_before_rebase_or_push(self):
        remote = self.commit(self.writer, "remote.txt", "remote work")
        self.git(self.writer, "push", "origin", "dison/prod")
        before_local = self.git(self.local, "rev-parse", "HEAD")
        before_main = self.git(self.fork, "rev-parse", "main")
        self.advance_upstream()
        result = self.sync()
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn("本地尚未纳入", result.stdout)
        self.assertEqual(self.git(self.local, "rev-parse", "HEAD"), before_local)
        self.assertEqual(self.git(self.fork, "rev-parse", "dison/prod"), remote)
        self.assertEqual(self.git(self.fork, "rev-parse", "main"), before_main)

    def test_divergent_remote_is_not_overwritten(self):
        remote = self.commit(self.writer, "remote.txt", "remote work")
        self.git(self.writer, "push", "origin", "dison/prod")
        local = self.commit(self.local, "local.txt", "local work")
        result = self.sync()
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertEqual(self.git(self.local, "rev-parse", "HEAD"), local)
        self.assertEqual(self.git(self.fork, "rev-parse", "dison/prod"), remote)

    def test_normal_rebase_and_sh_entrypoint(self):
        self.commit(self.local, "local.txt", "local work")
        self.advance_upstream()
        result = self.sync(shell="sh")
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assert_contains(self.fork, "dison/prod", "local.txt", "local work")
        self.assert_contains(self.fork, "dison/prod", "upstream.txt", "upstream advance")
        self.assertEqual(self.git(self.fork, "rev-parse", "main"), self.git(self.upstream, "rev-parse", "main"))

    def test_equivalent_rebased_commits_can_be_retried(self):
        original = self.commit(self.local, "local.txt", "local work")
        self.git(self.local, "push", "fork", "dison/prod")
        self.advance_upstream()
        self.git(self.local, "fetch", "origin")
        self.git(self.local, "rebase", "origin/main")
        self.assertNotEqual(self.git(self.local, "rev-parse", "HEAD"), original)
        result = self.sync()
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assert_contains(self.fork, "dison/prod", "local.txt", "local work")

    def test_missing_remote_branch_uses_creation_lease(self):
        self.git(self.fork, "update-ref", "-d", "refs/heads/dison/prod")
        self.commit(self.local, "local.txt", "local work")
        result = self.sync()
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assert_contains(self.fork, "dison/prod", "local.txt", "local work")

    def test_background_fetch_cannot_relax_push_lease(self):
        remote = self.commit(self.writer, "remote.txt", "concurrent remote work")
        self.commit(self.local, "local.txt", "local work")
        wrapper_dir = self.root / "bin"
        wrapper_dir.mkdir()
        wrapper = wrapper_dir / "git"
        # Publish a concurrent commit and refresh the tracking ref immediately
        # before the script's push. An implicit lease would now accept it.
        wrapper.write_text(
            f"#!{sys.executable}\n"
            "import os, subprocess, sys\n"
            f"git = {GIT!r}\n"
            "args = sys.argv[1:]\n"
            "if args and args[0] == 'push' and any(a.startswith('--force-with-lease') for a in args):\n"
            f"    subprocess.run([git, '-C', {str(self.writer)!r}, 'push', 'origin', 'dison/prod'], check=True)\n"
            f"    subprocess.run([git, '-C', {str(self.local)!r}, 'fetch', 'fork'], check=True)\n"
            "os.execv(git, [git, *args])\n"
        )
        wrapper.chmod(0o755)
        env = {**self.env, "PATH": str(wrapper_dir) + os.pathsep + self.env.get("PATH", "")}
        result = self.sync(env=env)
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn("推送未完成", result.stdout)
        self.assertEqual(self.git(self.fork, "rev-parse", "dison/prod"), remote)
        self.assert_contains(self.local, "HEAD", "local.txt", "local work")


if __name__ == "__main__":
    unittest.main()
