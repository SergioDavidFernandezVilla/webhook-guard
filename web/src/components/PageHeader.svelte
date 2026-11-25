<script lang="ts">
  export let title = "";
  export let description = "";
  export let code = "";
</script>

<header class="page-head">
  <div>
    <h1 class="page-title">{title}</h1>
    {#if description}<p class="page-desc">{description}</p>{/if}
  </div>
  <div class="toolbar">
    {#if code}<span class="page-code">{code}</span>{/if}
    <slot />
  </div>
</header>
