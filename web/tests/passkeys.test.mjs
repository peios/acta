import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import ts from "typescript";

// Compile the browser boundary with the project's existing TypeScript compiler.
// The only import is a type, so no application or browser runtime is needed.
const source = await readFile(
  new URL("../src/lib/passkeys.ts", import.meta.url),
  "utf8",
);
const { outputText } = ts.transpileModule(source, {
  compilerOptions: {
    target: ts.ScriptTarget.ES2022,
    module: ts.ModuleKind.ESNext,
  },
});
const { browserPasskey } = await import(
  `data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`
);

function reactive(value) {
  if (!value || typeof value !== "object") return value;
  return new Proxy(value, {
    get: (target, key) => reactive(Reflect.get(target, key)),
  });
}
function setGlobal(t, name, value) {
  const previous = Object.getOwnPropertyDescriptor(globalThis, name);
  Object.defineProperty(globalThis, name, { value, configurable: true });
  t.after(() =>
    previous
      ? Object.defineProperty(globalThis, name, previous)
      : delete globalThis[name],
  );
}
const bytes = (...values) => new Uint8Array(values).buffer;

for (const creating of [false, true]) {
  test(`${creating ? "registration" : "sign-in"} accepts reactive options without mutating the flow`, async (t) => {
    const publicKey = {
      challenge: "AQID",
      [creating ? "excludeCredentials" : "allowCredentials"]: [
        { type: "public-key", id: "BAUG" },
      ],
      ...(creating
        ? {
            user: { id: "BwgJ", name: "jack", displayName: "Jack" },
            authenticatorSelection: { userVerification: "required" },
          }
        : { userVerification: "required" }),
    };
    const original = structuredClone(publicKey);
    const flow = reactive({
      purpose: creating ? "passkey_add" : "login",
      step: creating ? "passkey_create" : "passkey_verify",
      options: { publicKey },
    });
    // This is the failure the user saw: native structuredClone cannot copy a Proxy.
    assert.throws(() => structuredClone(flow.options.publicKey), {
      name: "DataCloneError",
    });
    const signal = new AbortController().signal;
    let calls = 0;
    class Attestation {
      clientDataJSON = bytes(10, 11);
      attestationObject = bytes(12, 13);
      getTransports() {
        return ["internal"];
      }
    }
    setGlobal(t, "window", {
      isSecureContext: true,
      PublicKeyCredential: class {},
    });
    setGlobal(t, "AuthenticatorAttestationResponse", Attestation);
    setGlobal(t, "navigator", {
      credentials: {
        [creating ? "create" : "get"]: async (request) => {
          calls++;
          assert.equal(request.signal, signal);
          assert.deepEqual([...request.publicKey.challenge], [1, 2, 3]);
          assert.deepEqual(
            [
              ...request.publicKey[
                creating ? "excludeCredentials" : "allowCredentials"
              ][0].id,
            ],
            [4, 5, 6],
          );
          if (creating)
            assert.deepEqual([...request.publicKey.user.id], [7, 8, 9]);
          else assert.equal(request.publicKey.userVerification, "required");
          assert.doesNotThrow(() => structuredClone(request.publicKey));
          return {
            id: "BAUG",
            rawId: bytes(4, 5, 6),
            type: "public-key",
            authenticatorAttachment: "platform",
            response: creating
              ? new Attestation()
              : {
                  clientDataJSON: bytes(10, 11),
                  authenticatorData: bytes(12, 13),
                  signature: bytes(14, 15),
                  userHandle: bytes(7, 8, 9),
                },
            getClientExtensionResults: () => ({}),
          };
        },
      },
    });
    const result = await browserPasskey(flow, creating, signal);
    assert.equal(calls, 1);
    assert.deepEqual(publicKey, original);
    assert.equal(result.rawId, "BAUG");
    assert.equal(result.response.clientDataJSON, "Cgs");
    if (creating) assert.equal(result.response.attestationObject, "DA0");
    else {
      assert.equal(result.response.signature, "Dg8");
      assert.equal(result.response.userHandle, "BwgJ");
    }
  });
}
