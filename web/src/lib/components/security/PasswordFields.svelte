<script lang="ts">
  import PasswordField from "../PasswordField.svelte";
  let {
    password = $bindable(""),
    newPassword = $bindable(""),
    busy = false,
    fields = {},
  }: {
    password?: string;
    newPassword?: string;
    busy?: boolean;
    fields?: Record<string, string>;
  } = $props();
</script>

<PasswordField
  bind:value={password}
  label="Current password"
  disabled={busy}
  error={fields.password}
/>
<PasswordField
  bind:value={newPassword}
  label="New password"
  creating
  disabled={busy}
  error={fields.new_password}
  hint="Use at least 15 characters. A few unrelated words work well."
/>
