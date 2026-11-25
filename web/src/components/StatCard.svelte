<script lang="ts">
  export let label = "";
  export let value: string | number = "";
  export let unit = "";
  export let hint = "";
</script>

<div class="stat">
  <span class="stat-label">{label}</span>
  <span class="stat-value">{value}{#if unit}<small>{unit}</small>{/if}</span>
  {#if hint}<span class="stat-hint">{hint}</span>{/if}
</div>
