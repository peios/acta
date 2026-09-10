import { api } from "./api";
export type Purpose =
  | "login"
  | "password"
  | "passkey_add"
  | "passkey_remove"
  | "mfa_setup"
  | "mfa_disable"
  | "mfa_policy"
  | "recovery";
export type Flow = {
  id?: string;
  purpose: Purpose;
  step: string;
  method?: string;
  account_id?: string;
  options?: { publicKey: Record<string, unknown> };
  secret?: string;
  qr?: string;
  codes?: string[];
};
export type SecurityState = {
  mfa: boolean;
  extra_code: boolean;
  recovery_remaining: number;
  passkeys: {
    id: string;
    name: string;
    created_at: string;
    last_used_at: string | null;
  }[];
  sessions: {
    kind: "browser" | "cli" | "mcp";
    tool_grants?: string[];
    id: string;
    description: string;
    created_at: string;
    last_seen_at: string;
    current: boolean;
  }[];
};
export const flowTitles: Record<Purpose, string> = {
  login: "Verify your sign-in",
  password: "Change password",
  passkey_add: "New passkey",
  passkey_remove: "Remove passkey",
  mfa_setup: "Set up an authenticator",
  mfa_disable: "Disable authenticator",
  mfa_policy: "Update sign-in protection",
  recovery: "New recovery codes",
};
export function beginFlow(purpose: Purpose, target = "", enabled = false) {
  return api<Flow>("security/flow", { purpose, target, enabled });
}
export function advanceFlow(
  flow: Flow,
  action: string,
  fields: Record<string, unknown> = {},
) {
  return api<Flow>("security/advance", { id: flow.id, action, ...fields });
}
export function cancelFlow(flow: Flow) {
  return api("security/cancel", { id: flow.id });
}
export function rememberPasskeyChoice(accountID: string) {
  try {
    localStorage.setItem(`acta.passkey-suggestion.${accountID}`, "dismissed");
  } catch {
    /* Optional browser preference. */
  }
}
export function offerPasskey(accountID: string) {
  try {
    return (
      localStorage.getItem(`acta.passkey-suggestion.${accountID}`) !==
      "dismissed"
    );
  } catch {
    return true;
  }
}
