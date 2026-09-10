<script lang="ts">
  let {
    value = $bindable(""),
    creating = false,
    error = "",
    hint = "",
    disabled = false,
    label = "Password",
  }: {
    value?: string;
    creating?: boolean;
    error?: string;
    hint?: string;
    disabled?: boolean;
    label?: string;
  } = $props();
  let visible = $state(false);
  const id = $props.id();
</script>

<div class="field">
  <label for={id}>{label}</label>
  <div class="password-input">
    <input
      {id}
      name={creating ? "new_password" : "password"}
      type={visible ? "text" : "password"}
      bind:value
      autocomplete={creating ? "new-password" : "current-password"}
      required
      {disabled}
      aria-invalid={error ? true : undefined}
      aria-describedby={[hint ? `${id}-hint` : "", error ? `${id}-error` : ""]
        .filter(Boolean)
        .join(" ") || undefined}
    />
    <button
      type="button"
      class="reveal"
      onclick={() => (visible = !visible)}
      aria-label={visible ? "Hide password" : "Show password"}
      aria-pressed={visible}
      {disabled}>{visible ? "Hide" : "Show"}</button
    >
  </div>
  {#if hint}<p class="hint" id={`${id}-hint`}>{hint}</p>{/if}
  {#if error}<p class="field-error" id={`${id}-error`}>{error}</p>{/if}
</div>
