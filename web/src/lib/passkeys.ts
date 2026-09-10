import type { Flow } from "./security";

function decode(raw: unknown): Uint8Array<ArrayBuffer> {
  const str = String(raw).replace(/-/g, "+").replace(/_/g, "/");
  return Uint8Array.from(atob(str), (char) => char.charCodeAt(0));
}
function encode(raw: ArrayBuffer): string {
  return btoa(String.fromCharCode(...new Uint8Array(raw)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}
export async function browserPasskey(
  flow: Flow,
  creating: boolean,
  signal: AbortSignal,
): Promise<Record<string, unknown>> {
  if (!window.isSecureContext || !window.PublicKeyCredential)
    throw new Error(
      "Passkeys need a supported browser on HTTPS or localhost. You can use your password instead.",
    );
  if (!flow.options?.publicKey)
    throw new Error("The passkey request expired. Start again.");
  // Options arrive as JSON but may have passed through Svelte's deep reactive
  // proxies. Clone that JSON representation before decoding binary fields;
  // structuredClone rejects proxies, and decoding must not mutate flow state.
  const options = JSON.parse(JSON.stringify(flow.options.publicKey)) as Record<
    string,
    any
  >;
  options.challenge = decode(options.challenge);
  for (const key of ["allowCredentials", "excludeCredentials"])
    if (options[key])
      options[key] = options[key].map((item: Record<string, unknown>) => ({
        ...item,
        id: decode(item.id),
      }));
  if (creating) options.user.id = decode(options.user.id);
  const credential = (await (creating
    ? navigator.credentials.create({
        publicKey: options as PublicKeyCredentialCreationOptions,
        signal,
      })
    : navigator.credentials.get({
        publicKey: options as PublicKeyCredentialRequestOptions,
        signal,
      }))) as PublicKeyCredential | null;
  if (!credential)
    throw new Error("No passkey was returned. You can try again.");
  const response = credential.response;
  const payload: Record<string, unknown> = {
    clientDataJSON: encode(response.clientDataJSON),
  };
  if (response instanceof AuthenticatorAttestationResponse) {
    payload.attestationObject = encode(response.attestationObject);
    payload.transports = response.getTransports();
  } else {
    const assertion = response as AuthenticatorAssertionResponse;
    payload.authenticatorData = encode(assertion.authenticatorData);
    payload.signature = encode(assertion.signature);
    payload.userHandle = assertion.userHandle
      ? encode(assertion.userHandle)
      : null;
  }
  return {
    id: credential.id,
    rawId: encode(credential.rawId),
    type: credential.type,
    response: payload,
    clientExtensionResults: credential.getClientExtensionResults(),
    authenticatorAttachment: credential.authenticatorAttachment,
  };
}
