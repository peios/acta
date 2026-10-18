"""Check maintained Python, shell and Starlark files against the PSC profiles."""

import argparse
import os
import subprocess
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    for tool in ("ruff", "shfmt", "shellcheck", "buildifier"):
        parser.add_argument(f"--{tool}", required=True, type=Path)
    parser.add_argument(
        "--fix", action="store_true", help="Apply formatting; still report lint errors"
    )
    args = parser.parse_args()
    tools = {
        name: str(getattr(args, name).resolve())
        for name in ("ruff", "shfmt", "shellcheck", "buildifier")
    }
    root = Path(os.environ["BUILD_WORKSPACE_DIRECTORY"])
    # Include new maintained files during development without walking dependencies
    # or build outputs. SQL migration history is not a rewrite target.
    files = (
        subprocess.check_output(
            ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"], cwd=root
        )
        .decode()
        .split("\0")
    )
    files = sorted({name for name in files if name and (root / name).is_file()})
    python = [name for name in files if name.endswith(".py")]
    shell = [name for name in files if name.endswith(".sh") or name == "bazel"]
    starlark = [name for name in files if name.endswith((".bzl", ".bazel"))]
    commands = [
        [tools["ruff"], "check", "--no-cache", *python],
        [tools["ruff"], "format", "--no-cache", *([] if args.fix else ["--check"]), *python],
        [tools["shfmt"], "-w" if args.fix else "-d", "-i", "4", *shell],
        [tools["shellcheck"], *shell],
        [
            tools["buildifier"],
            "-mode=fix" if args.fix else "-mode=check",
            "-lint=warn",
            "-warnings=load,unused-variable,function-docstring",
            *starlark,
        ],
    ]
    failed = False
    for command in commands:
        result = subprocess.run(command, cwd=root, check=False)
        failed = failed or result.returncode != 0
    raise SystemExit(1 if failed else 0)


if __name__ == "__main__":
    main()
