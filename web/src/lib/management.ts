import type { Account } from "./api";
export type ManagedAccount = Account & {
  status: "active" | "pending" | "disabled";
  pending: boolean;
  created_at: string;
};
// One-navigation handoff: invitation secrets are never put into route/history
// state or persistent browser storage on the administrator's side.
const invitations = new Map<string, string>();
export function handoffInvitation(id: string, url: string) {
  invitations.set(id, url);
}
export function takeInvitation(id: string) {
  const url = invitations.get(id);
  invitations.delete(id);
  return url;
}
