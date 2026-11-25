<script lang="ts">
  import { ApiError } from "../lib/api";
  import { dict } from "../i18n";

  export let error: unknown = null;
  export let retry: (() => void) | null = null;

  $: code = error instanceof ApiError ? error.code : "network";
  $: detail = error instanceof ApiError ? `HTTP ${error.status}` : error instanceof Error ? error.message : "";
  $: message = $dict.errors[code as keyof typeof $dict.errors] ?? $dict.errors.generic;
</script>

<div class="card">
  <div class="card-body stack">
    <div class="callout" data-tone="danger">
      <div>
        <strong>{$dict.common.errorTitle}</strong>
        <p style="margin:.25rem 0 0">{$dict.common.errorBody}</p>
        <p style="margin:.5rem 0 0" class="mono">{message}{#if detail} · {detail}{/if}</p>
      </div>
    </div>
    {#if retry}
      <div>
        <button type="button" class="btn btn-primary" on:click={retry}>{$dict.common.retry}</button>
      </div>
    {/if}
  </div>
</div>
