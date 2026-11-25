<script lang="ts">
  import { dict } from "../i18n";

  export let offset = 0;
  export let limit = 25;
  export let total = 0;
  export let onchange: (offset: number) => void = () => {};

  $: pages = Math.max(1, Math.ceil(total / limit));
  $: page = Math.floor(offset / limit) + 1;
  $: from = total === 0 ? 0 : offset + 1;
  $: to = Math.min(offset + limit, total);
</script>

<div class="pagination">
  <span class="pagination-info" role="status" aria-live="polite">
    {$dict.common.showing} {from}–{to} {$dict.common.of} {total} {$dict.common.results}
  </span>
  <div class="toolbar" role="group" aria-label={$dict.common.page}>
    <button
      type="button"
      class="btn btn-sm"
      disabled={offset <= 0}
      on:click={() => onchange(Math.max(0, offset - limit))}
    >
      {$dict.common.previous}
    </button>
    <span class="pagination-info">{$dict.common.page} {page} / {pages}</span>
    <button
      type="button"
      class="btn btn-sm"
      disabled={offset + limit >= total}
      on:click={() => onchange(offset + limit)}
    >
      {$dict.common.next}
    </button>
  </div>
</div>
