import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import ts from "typescript";
const source = await readFile(
  new URL("../src/lib/auth-return.ts", import.meta.url),
  "utf8",
);
const { outputText } = ts.transpileModule(source, {
  compilerOptions: {
    target: ts.ScriptTarget.ES2022,
    module: ts.ModuleKind.ESNext,
  },
});
const { authReturn } = await import(
  `data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`
);

test("sign-in resumes connection flows without accepting open redirects", () => {
  for (const path of ["/login/device?code=ABCD", "/login/oauth?request=xyz"])
    assert.equal(authReturn(path), path);
  for (const path of [
    null,
    "",
    "https://evil.example/login/oauth",
    "//evil.example/login/oauth",
    "/\\evil.example/login/oauth",
    "/login/oauth/../../site-settings/users",
    "/login/device-evil",
    "/site-settings/users",
  ])
    assert.equal(authReturn(path, "/fallback"), "/fallback");
});
