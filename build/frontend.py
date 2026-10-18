"""Stage declared frontend inputs in scratch space; never write into the checkout."""

import argparse
import json
import os
import shutil
import subprocess
import tempfile
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--node", type=Path, required=True)
    parser.add_argument("--npm", type=Path, required=True)
    parser.add_argument(
        "--mode", choices=["build", "check", "format", "test", "lint"], required=True
    )
    parser.add_argument("--output", type=Path)
    parser.add_argument("--version", default="0.0.0")
    args = parser.parse_args()
    node = args.node.resolve()
    npm = args.npm.resolve()
    dependencies = node.parents[2] / "node_modules"
    output = args.output.absolute() if args.output else None
    sources = json.loads(args.manifest.read_text())
    with tempfile.TemporaryDirectory(prefix="acta-web-", dir=os.environ.get("TEST_TMPDIR")) as temp:
        root = Path(temp)
        for relative, source in sources.items():
            target = root / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source, target)
        web = root / "web"
        (web / "node_modules").symlink_to(dependencies, target_is_directory=True)
        env = {
            "PATH": f"{node.parent}:/usr/bin:/bin",
            "HOME": str(root),
            "TMPDIR": str(root),
            "CI": "true",
            "TZ": "UTC",
            "LANG": "C.UTF-8",
            "NPM_CONFIG_USERCONFIG": "/dev/null",
            "NPM_CONFIG_GLOBALCONFIG": str(root / ".globalnpmrc"),
            "npm_config_update_notifier": "false",
        }
        if args.mode == "build":
            package = web / "package.json"
            data = json.loads(package.read_text())
            data["version"] = args.version.removeprefix("v")
            package.write_text(json.dumps(data) + "\n")
        script = {
            "build": "build",
            "check": "check",
            "format": "format:check",
            "test": "test",
            "lint": "lint",
        }[args.mode]
        subprocess.run([str(node), str(npm), "run", script], cwd=web, env=env, check=True)
        if output is not None:
            shutil.copytree(web / "build", output, dirs_exist_ok=True)


if __name__ == "__main__":
    main()
