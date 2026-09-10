import { api, type Account } from "./api";
import { checkPermission } from "./permissions";
export type Group = {
  id: string;
  name: string;
  description: string;
  is_default: boolean;
  direct_permissions: string[];
  direct_require_mfa: boolean;
  permissions_version: number;
  member_count: number;
  assignable: boolean;
};
export function groupAssignable(actor: Account, group: Group): boolean {
  return group.direct_permissions.every((p) => checkPermission(actor, p));
}
export async function changeMembership(
  group: Group,
  account: Account,
  member: boolean,
) {
  await api(`groups/${group.id}/members/${account.id}`, {
    member,
    group_version: group.permissions_version,
    permissions_version: account.permissions_version,
  });
  window.dispatchEvent(new Event("acta:permissions-changed"));
}

export function isLastSuperuserSource(account: Account, group: Group): boolean {
  return (
    account.last_active_superuser &&
    group.direct_permissions.includes("site.superuser") &&
    !account.direct_permissions.includes("site.superuser") &&
    !account.groups.some(
      (g) =>
        g.id !== group.id && g.direct_permissions.includes("site.superuser"),
    )
  );
}
