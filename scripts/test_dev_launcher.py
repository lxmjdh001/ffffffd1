#!/usr/bin/env python3
"""Exercise the native launcher in temporary fixtures without real configuration."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


LAUNCHER = Path(__file__).resolve().parent / "dev.sh"


class NativeCoverLauncherTests(unittest.TestCase):
    def run_fixture(self, role=None, check_result=0):
        with tempfile.TemporaryDirectory(prefix="d1-native-launcher-") as directory:
            root = Path(directory)
            (root / "scripts").mkdir()
            (root / "web/node_modules").mkdir(parents=True)
            shutil.copyfile(LAUNCHER, root / "scripts/dev.sh")
            config = "LANDING_SCREENSHOT_BROWSER='/fixture/chrome with spaces'\n"
            if role is not None:
                config += f"SERVER_ROLE={role}\n"
            (root / ".env.development").write_text(config)
            commands = root / "commands"
            commands.mkdir()
            log = root / "commands.log"
            binary = root / "fixture-binary"
            binary.write_text(
                "#!/bin/sh\n"
                "if [ \"${1:-}\" = check-landing-browser ]; then\n"
                "  printf 'check browser=%s\\n' \"$LANDING_SCREENSHOT_BROWSER\" >> \"$D1_LAUNCH_TEST_LOG\"\n"
                "  exit \"$D1_LAUNCH_TEST_CHECK_RESULT\"\n"
                "fi\n"
                "printf 'server\\n' >> \"$D1_LAUNCH_TEST_LOG\"\n"
            )
            stubs = {
                "npm": "#!/bin/sh\nprintf 'build-web\\n' >> \"$D1_LAUNCH_TEST_LOG\"\n",
                "go": "#!/bin/sh\nprintf 'build-go\\n' >> \"$D1_LAUNCH_TEST_LOG\"\ncp \"$D1_LAUNCH_TEST_BINARY\" bin/d1apk\nchmod +x bin/d1apk\n",
            }
            for name, script in stubs.items():
                path = commands / name
                path.write_text(script)
                path.chmod(0o755)
            result = subprocess.run(
                ["/bin/bash", str(root / "scripts/dev.sh")],
                cwd=root,
                env={
                    "PATH": str(commands) + os.pathsep + "/usr/bin:/bin",
                    "D1_LAUNCH_TEST_BINARY": str(binary),
                    "D1_LAUNCH_TEST_LOG": str(log),
                    "D1_LAUNCH_TEST_CHECK_RESULT": str(check_result),
                },
                capture_output=True,
                text=True,
                timeout=10,
            )
            return result, log.read_text().splitlines()

    def test_default_all_checks_real_capture_before_start(self):
        result, commands = self.run_fixture()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(commands, ["build-web", "build-go", "check browser=/fixture/chrome with spaces", "server"])

    def test_control_check_failure_stops_before_server(self):
        result, commands = self.run_fixture(role="control", check_result=7)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(commands, ["build-web", "build-go", "check browser=/fixture/chrome with spaces"])
        self.assertIn("真实封面截图检查失败", result.stderr)
        self.assertIn("LANDING_SCREENSHOT_BROWSER", result.stderr)

    def test_download_skips_browser_requirement(self):
        result, commands = self.run_fixture(role="download", check_result=7)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(commands, ["build-web", "build-go", "server"])


if __name__ == "__main__":
    unittest.main()
