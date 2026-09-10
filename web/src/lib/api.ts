export type SetupState = {
  complete: boolean;
  unlocked: boolean;
  password_min: number;
  password_max: number;
};
import type { Group } from "./groups";

export type Account = {
  id: string;
  username: string;
  username_segment?: string;
  owner_id?: string | null;
  owner_username?: string;
  display_name: string | null;
  permissions: string[];
  direct_permissions: string[];
  require_mfa: boolean;
  direct_require_mfa: boolean;
  groups: Group[];
  can_create_users: boolean;
  mfa_setup_required: boolean;
  permissions_version: number;
  last_active_superuser: boolean;
  profile_version: number;
  previous_usernames: string[];
};

export class APIError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public fields: Record<string, string> = {},
  ) {
    super(message);
  }
}

export async function api<T>(
  path: string,
  body?: unknown,
  options: { signal?: AbortSignal } = {},
): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`/api/${path}`, {
      method: body === undefined ? "GET" : "POST",
      credentials: "same-origin",
      signal: options.signal,
      headers:
        body === undefined || body instanceof FormData
          ? {}
          : { "Content-Type": "application/json" },
      body:
        body === undefined
          ? undefined
          : body instanceof FormData
            ? body
            : JSON.stringify(body),
    });
  } catch (error) {
    if (options.signal?.aborted) throw error;
    throw new APIError(
      0,
      "offline",
      "We couldn’t reach Acta. Check your connection and try again.",
    );
  }
  const data = await response.json().catch(() => null);
  if (!response.ok) {
    if (
      typeof window !== "undefined" &&
      data?.error?.code === "unauthenticated"
    )
      window.dispatchEvent(new Event("acta:session-expired"));
    if (
      typeof window !== "undefined" &&
      data?.error?.code === "account_disabled"
    )
      window.dispatchEvent(new Event("acta:account-disabled"));
    if (typeof window !== "undefined" && data?.error?.code === "mfa_required")
      window.dispatchEvent(new Event("acta:mfa-required"));
    if (typeof window !== "undefined" && data?.error?.code === "forbidden")
      window.dispatchEvent(new Event("acta:permissions-changed"));
    throw new APIError(
      response.status,
      data?.error?.code ?? "unavailable",
      data?.error?.message ?? "Acta is unavailable. Please try again.",
      data?.error?.fields ?? {},
    );
  }
  if (data === null)
    throw new APIError(
      502,
      "invalid_response",
      "Acta returned an unexpected response. Please try again.",
    );
  return data as T;
}

export function errorMessage(error: unknown): string {
  return error instanceof Error
    ? error.message
    : "Something went wrong. Please try again.";
}
