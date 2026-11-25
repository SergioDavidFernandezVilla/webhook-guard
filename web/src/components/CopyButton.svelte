<script lang="ts">
  import { dict } from "../i18n";

  export let value = "";
  export let label = "";

  let copied = false;
  let failed = false;
  let timer: ReturnType<typeof setTimeout> | undefined;

  async function copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(value);
      copied = true;
      failed = false;
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => {
        copied = false;
      }, 1500);
    } catch {
      copied = false;
      failed = true;
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => {
        failed = false;
      }, 3000);
    }
  }

  $: aria = copied ? $dict.common.copied : failed ? $dict.common.copyFailed : (label || $dict.common.copy);
  $: announcement = copied ? $dict.common.copied : failed ? $dict.common.copyFailed : "";
</script>

<button type="button" class="btn btn-ghost btn-sm" on:click={copy} aria-label={aria} title={aria}>
  {#if copied}
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
      <path d="M20 6 9 17l-5-5" />
    </svg>
  {:else if failed}
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
      <path d="M12 8v5M12 17h.01" />
      <circle cx="12" cy="12" r="9" />
    </svg>
  {:else}
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
      <rect x="9" y="9" width="11" height="11" rx="2" />
      <path d="M5 15V5a2 2 0 0 1 2-2h10" />
    </svg>
  {/if}
</button>
<span class="visually-hidden" role="status" aria-live="polite">{announcement}</span>
