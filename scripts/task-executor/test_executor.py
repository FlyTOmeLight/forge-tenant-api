"""Executor scope and command boundaries; no model, GitLab, or repository writes."""
import importlib.util
import os
from pathlib import Path
import tempfile
import subprocess
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("executor", Path(__file__).with_name("executor.py"))
executor = importlib.util.module_from_spec(spec)
with tempfile.TemporaryDirectory() as root:
    with patch.dict(os.environ, {"WORKSPACE_ROOT": root, "GITLAB_PROJECT_ID": "77", "FORGE_URL": "http://unused", "GITLAB_URL": "http://unused"}):
        spec.loader.exec_module(executor)

class ExecutorTest(unittest.TestCase):
    def test_scope_rejects_escape_and_unrelated_files(self):
        for path, allowed in [("internal/a.go", True), ("internal/nested/a.go", True), ("../a.go", False), ("/internal/a.go", False), ("internal/../secret", False), (".git/config", False), ("internal-other/a.go", False), ("internal\\a.go", False)]:
            with self.subTest(path=path):
                self.assertEqual(executor.in_scope(path, ["internal/"]), allowed)

    def test_configured_commands_are_argv_not_shell(self):
        commands = ["go test ./internal/pkg -count=1", "python3 -c 'print(1)'", "echo ok; echo hidden"]
        with patch.object(executor, "gate"), patch.object(executor.subprocess, "run", return_value=subprocess.CompletedProcess([],0,"done","")) as run:
            result = executor.checked_commands({"config": {"checks": commands}}, Path("/tmp"))
        self.assertEqual([c.args[0] for c in run.call_args_list], [["go", "test", "./internal/pkg", "-count=1"], ["python3", "-c", "print(1)"], ["echo", "ok;", "echo", "hidden"]])
        self.assertTrue(all(c["passed"] for c in result))
        self.assertTrue(all("SERVICE_TOKEN" not in c.kwargs["env"] for c in run.call_args_list))

if __name__ == "__main__":
    unittest.main()
