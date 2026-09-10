import { getContext, setContext } from "svelte";
import type { Account } from "./api";

const key = Symbol("signed-in account");
type AccountContext = {
  readonly account: Account;
  update: (account: Account) => void;
};
export function provideAccount(value: AccountContext) {
  setContext(key, value);
}
export function useAccount(): AccountContext {
  return getContext(key);
}
