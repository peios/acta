"""Fetch locked npm dependencies once, outside build and test actions."""

def _npm_dependencies_impl(ctx):
    if ctx.os.name != "linux" or ctx.os.arch not in ["amd64", "x86_64"]:
        fail("Acta's initial Bazel frontend toolchain supports x86_64 Linux.")
    ctx.download_and_extract(
        url = "https://nodejs.org/dist/v22.22.1/node-v22.22.1-linux-x64.tar.xz",
        sha256 = "9a6bc82f9b491279147219f6a18add1e18424dce90d41d2a5fcd69d4924ba3aa",
        stripPrefix = "node-v22.22.1-linux-x64",
        output = "node",
    )
    ctx.file("package.json", ctx.read(ctx.attr.package_json))
    ctx.file("package-lock.json", ctx.read(ctx.attr.package_lock))
    result = ctx.execute(
        [
            str(ctx.path("node/bin/node")),
            str(ctx.path("node/lib/node_modules/npm/bin/npm-cli.js")),
            "ci",
            "--ignore-scripts",
            "--no-audit",
            "--no-fund",
            "--cache",
            str(ctx.path(".npm-cache")),
        ],
        environment = {
            "PATH": str(ctx.path("node/bin")) + ":/usr/bin:/bin",
            "HOME": str(ctx.path(".home")),
            "NPM_CONFIG_USERCONFIG": "/dev/null",
            "NPM_CONFIG_GLOBALCONFIG": str(ctx.path(".globalnpmrc")),
        },
        timeout = 600,
        quiet = False,
    )
    if result.return_code:
        fail("npm ci failed:\n" + result.stdout + result.stderr)
    ctx.delete(".npm-cache")
    ctx.delete(".home")
    ctx.file("BUILD.bazel", """
package(default_visibility = ["//visibility:public"])
exports_files(["node/bin/node", "node/lib/node_modules/npm/bin/npm-cli.js", "package.json"])
filegroup(name = "files", srcs = glob(["node/**", "node_modules/**"], exclude = ["**/BUILD", "**/BUILD.bazel"], allow_empty = False))
""")

npm_dependencies = repository_rule(
    implementation = _npm_dependencies_impl,
    attrs = {
        "package_json": attr.label(mandatory = True, allow_single_file = True),
        "package_lock": attr.label(mandatory = True, allow_single_file = True),
    },
)
