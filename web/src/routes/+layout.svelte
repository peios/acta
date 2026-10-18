<script lang="ts">
    import { onMount, tick } from "svelte";
    import { goto } from "$app/navigation";
    import { page } from "$app/state";

    import { focusIndicators } from "$lib/focus-indicators";
    onMount(focusIndicators);

    import "$lib/styles.css";
    import ThemeToggle from "$lib/components/ThemeToggle.svelte";

    import { rememberAuthReturn } from "$lib/auth-return";
    let { children } = $props();
    let blocked = $state(false);
    onMount(() => {
        const disabled = async () => {
            blocked = true;
            await tick();
            await goto("/account-disabled", { replaceState: true });
            blocked = false;
        };
        const required = async () => {
            if (["/login/device", "/login/oauth"].includes(page.url.pathname)) {
                rememberAuthReturn(page.url.pathname + page.url.search);
            }
            blocked = true;
            await tick();
            await goto("/mfa-required", { replaceState: true });
            blocked = false;
        };
        window.addEventListener("acta:mfa-required", required);
        window.addEventListener("acta:account-disabled", disabled);
        return () => {
            window.removeEventListener("acta:account-disabled", disabled);
            window.removeEventListener("acta:mfa-required", required);
        };
    });
</script>

{#if ["/login/oauth", "/login/device", "/login", "/setup", "/account-link", "/account-disabled", "/mfa-required"].includes(page.url.pathname)}<ThemeToggle
    />{/if}
{#if !blocked}{@render children()}{/if}
