"""Pinned Linux x86_64 tools for the PSC Python, shell and Starlark profiles."""

def _style_tools_impl(ctx):
    if ctx.os.name != "linux" or ctx.os.arch not in ["amd64", "x86_64"]:
        fail("Acta's initial style toolchain supports x86_64 Linux.")
    ctx.download(
        url = "https://github.com/mvdan/sh/releases/download/v3.14.1/shfmt_v3.14.1_linux_amd64",
        sha256 = "76e77641faa025814b77f153b29796b8e6fa2fca03e0c76a691608b86c7ea7bf",
        output = "shfmt",
        executable = True,
    )
    ctx.download(
        url = "https://github.com/bazel-contrib/buildtools/releases/download/v10.1.0/buildifier-linux-amd64",
        sha256 = "31b6a8aa1e5c746696788f428729701770ad91925873d8256cb885c60e12c77e",
        output = "buildifier",
        executable = True,
    )
    ctx.download_and_extract(
        url = "https://github.com/koalaman/shellcheck/releases/download/v0.11.0/shellcheck-v0.11.0.linux.x86_64.tar.gz",
        sha256 = "b7af85e41cc99489dcc21d66c6d5f3685138f06d34651e6d34b42ec6d54fe6f6",
        stripPrefix = "shellcheck-v0.11.0",
    )
    ctx.download_and_extract(
        url = "https://files.pythonhosted.org/packages/2c/3c/4a01195d93420cad1175bedad13a515dc8a56f95a6e39789b92e582682f5/ruff-0.17.0-py3-none-manylinux_2_17_x86_64.manylinux2014_x86_64.whl",
        sha256 = "bc73e7c133e82d55b5f15897b2a442d72c0cb4a0c886c46801ce3c247150b60c",
        type = "zip",
        output = "ruff-wheel",
    )
    ctx.symlink("ruff-wheel/ruff-0.17.0.data/scripts/ruff", "ruff")
    ctx.file("BUILD.bazel", """
exports_files(["ruff", "shfmt", "shellcheck", "buildifier"])
""")

style_tools = repository_rule(implementation = _style_tools_impl)
