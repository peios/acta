import type { Account } from "./api";

// The server resolves effective grants. UI consumers never expand Superuser or
// compose direct/group grants themselves.
export function checkPermission(account: Account, permission: string): boolean {
  return account.permissions.includes(permission);
}
export function canOpenUsers(account: Account): boolean {
  return (
    checkPermission(account, "site.users.view") ||
    checkPermission(account, "site.users.create")
  );
}
export function canIssueLink(
  actor: Account,
  target: Account & { pending: boolean },
): boolean {
  return (
    checkPermission(
      actor,
      target.pending ? "site.users.create" : "site.users.reset_credentials",
    ) &&
    (!checkPermission(target, "site.permissions.manage") ||
      checkPermission(actor, "site.superuser"))
  );
}
export type Permission = {
  id: string;
  category: string;
  label: string;
  description: string;
};

export function canOpenSite(account: Account): boolean {
  return (
    canOpenUsers(account) ||
    checkPermission(account, "site.permissions.manage") ||
    checkPermission(account, "site.backups.manage")
  );
}
