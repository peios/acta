import { getContext, setContext } from "svelte";

const key = Symbol("app navigation");
type Navigation = { open: () => void };

export function provideNavigation(navigation: Navigation) {
  setContext(key, navigation);
}

export function useNavigation(): Navigation {
  return getContext(key);
}
