<script lang="ts">
  export let title = "";
  export let body = "";
</script>

<div class="empty">
  <p class="empty-title">{title}</p>
  <p class="empty-body">{body}</p>
  <slot />
</div>
