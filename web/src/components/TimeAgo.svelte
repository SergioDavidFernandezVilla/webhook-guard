<script lang="ts">
  import { onMount } from "svelte";
  import { formatDateTime, relativeTime } from "../lib/format";
  import { dict, locale } from "../i18n";

  export let iso: string | null | undefined = null;

  let now = Date.now();
  let timer: ReturnType<typeof setInterval> | undefined;

  onMount(() => {
    timer = setInterval(() => {
      now = Date.now();
    }, 30000);
    return () => {
      if (timer) clearInterval(timer);
    };
  });

  $: title = iso ? formatDateTime(iso, $locale) : $dict.common.notAvailable;
  $: text = iso && now ? relativeTime(iso, $locale) : $dict.common.notAvailable;
</script>

<time class="relative-time" datetime={iso ?? undefined} title={title}>{text}</time>
