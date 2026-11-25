<script lang="ts">
  import { dict } from "../i18n";

  export let rows = 4;
  export let height = 14;
  export let label = "";

  $: text = label || $dict.common.loading;
</script>

<div class="stack" role="status" aria-live="polite" aria-busy="true" style="padding:.25rem 0">
  <span class="visually-hidden">{text}</span>
  <div aria-hidden="true" class="stack" style="gap:0.65rem">
    {#each Array(rows) as _, index}
      <div
        class="skeleton"
        style={`height:${height}px;width:${index % 3 === 0 ? "62%" : index % 3 === 1 ? "86%" : "74%"}`}
      ></div>
    {/each}
  </div>
</div>
