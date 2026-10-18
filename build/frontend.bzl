"""Run Vite and frontend checks with declared sources and locked dependencies."""

def _inputs(ctx):
    return depset(
        ctx.files.srcs + [ctx.file._runner, ctx.file._node, ctx.file._npm],
        transitive = [ctx.attr._dependencies[DefaultInfo].files],
    )

def _arguments(ctx, manifest):
    return [
        ctx.file._runner.path,
        "--manifest",
        manifest.path,
        "--node",
        ctx.file._node.path,
        "--npm",
        ctx.file._npm.path,
    ]

def _manifest(ctx):
    manifest = ctx.actions.declare_file(ctx.label.name + ".sources.json")
    ctx.actions.write(manifest, json.encode({f.short_path: f.path for f in ctx.files.srcs}))
    return manifest

def _frontend_impl(ctx):
    manifest = _manifest(ctx)
    output = ctx.actions.declare_directory("build")
    ctx.actions.run(
        executable = "/usr/bin/python3",
        arguments = _arguments(ctx, manifest) + [
            "--mode",
            "build",
            "--output",
            output.path,
            "--version",
            ctx.var.get("ACTA_VERSION", "0.0.0"),
        ],
        inputs = depset([manifest], transitive = [_inputs(ctx)]),
        outputs = [output],
        mnemonic = "ViteBuild",
        progress_message = "Building Acta's embedded frontend",
    )
    return [DefaultInfo(files = depset([output]))]

def _frontend_test_impl(ctx):
    # Runfiles paths differ from execution-root paths; keep a dedicated manifest.
    manifest = ctx.actions.declare_file(ctx.label.name + ".sources.json")
    ctx.actions.write(manifest, json.encode({f.short_path: f.short_path for f in ctx.files.srcs}))
    executable = ctx.actions.declare_file(ctx.label.name + ".sh")
    ctx.actions.write(executable, """#!/bin/bash
set -euo pipefail
cd "$TEST_SRCDIR/$TEST_WORKSPACE"
exec /usr/bin/python3 '%s' --manifest '%s' --node '%s' --npm '%s' --mode '%s'
""" % (
        ctx.file._runner.short_path,
        manifest.short_path,
        ctx.file._node.short_path,
        ctx.file._npm.short_path,
        ctx.attr.mode,
    ), is_executable = True)
    return [DefaultInfo(executable = executable, runfiles = ctx.runfiles(
        files = [manifest],
        transitive_files = _inputs(ctx),
    ))]

_ATTRS = {
    "srcs": attr.label_list(allow_files = True),
    "_runner": attr.label(default = "//build:frontend.py", allow_single_file = True),
    "_node": attr.label(default = "@acta_npm//:node/bin/node", allow_single_file = True),
    "_npm": attr.label(default = "@acta_npm//:node/lib/node_modules/npm/bin/npm-cli.js", allow_single_file = True),
    "_dependencies": attr.label(default = "@acta_npm//:files"),
}

frontend = rule(implementation = _frontend_impl, attrs = _ATTRS)
frontend_test = rule(
    implementation = _frontend_test_impl,
    attrs = dict(_ATTRS, mode = attr.string(mandatory = True, values = ["check", "format", "test", "lint"])),
    test = True,
)
