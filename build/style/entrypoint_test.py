"""Exercise application startup without reading secrets or starting services."""

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ENTRYPOINT = Path(sys.argv.pop(1)).resolve()


class EntrypointTest(unittest.TestCase):
    def run_entrypoint(self, fail_read: bool) -> tuple[int, bool, str]:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            secrets = root / "secrets"
            secrets.mkdir()
            for name in ("database-url", "security-key", "backup-token"):
                (secrets / name).write_text("test-only\n")
            commands = root / "bin"
            commands.mkdir()
            marker = root / "started"
            scripts = {
                "chown": "exit 0",
                "cat": "exit 23" if fail_read else "printf '%s' 'postgres://test-only'",
                "gosu": 'printf "%s" "$ACTA_DATABASE_URL" > "$START_MARKER"',
            }
            for name, body in scripts.items():
                command = commands / name
                command.write_text("#!/bin/sh\n" + body + "\n")
                command.chmod(0o755)
            # Relocate the script's fixed runtime paths into this test's private tree.
            script = ENTRYPOINT.read_text().replace("/run/secrets", str(secrets))
            script = script.replace("/run/acta", str(root / "runtime"))
            result = subprocess.run(
                ["/bin/sh", "-s", "--", "acta-server"],
                input=script,
                text=True,
                capture_output=True,
                env={"PATH": f"{commands}:/usr/bin:/bin", "START_MARKER": str(marker)},
                check=False,
            )
            return result.returncode, marker.exists(), marker.read_text() if marker.exists() else ""

    def test_failed_database_url_read_prevents_launch(self) -> None:
        status, started, _ = self.run_entrypoint(fail_read=True)
        self.assertEqual(status, 23)
        self.assertFalse(started)

    def test_database_url_is_exported_before_launch(self) -> None:
        status, started, database_url = self.run_entrypoint(fail_read=False)
        self.assertEqual(status, 0)
        self.assertTrue(started)
        self.assertEqual(database_url, "postgres://test-only")


if __name__ == "__main__":
    unittest.main()
