<script lang="ts">
  import { onMount } from "svelte";
  import { dict } from "../i18n";

  export let title = "";
  export let onclose: () => void = () => {};

  let panel: HTMLElement | null = null;
  let host: HTMLDivElement | null = null;
  let shell: HTMLElement | null = null;
  let skip: HTMLElement | null = null;

  const FOCUSABLE =
    'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

  function focusables(): HTMLElement[] {
    if (!panel) return [];
    return Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
      (element) => element.offsetParent !== null || element === document.activeElement,
    );
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === "Escape") {
      event.preventDefault();
      onclose();
      return;
    }
    if (event.key !== "Tab") return;
    const items = focusables();
    if (items.length === 0) {
      event.preventDefault();
      panel?.focus();
      return;
    }
    const first = items[0];
    const last = items[items.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  onMount(() => {
    const previous = document.activeElement as HTMLElement | null;
    if (host) document.body.appendChild(host);
    shell = document.querySelector<HTMLElement>(".app-shell");
    skip = document.querySelector<HTMLElement>(".skip-link");
    shell?.setAttribute("inert", "");
    shell?.setAttribute("aria-hidden", "true");
    skip?.setAttribute("inert", "");
    panel?.focus();
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      shell?.removeAttribute("inert");
      shell?.removeAttribute("aria-hidden");
      skip?.removeAttribute("inert");
      document.body.style.overflow = previousOverflow;
      host?.remove();
      previous?.focus();
    };
  });
</script>

<svelte:window on:keydown={onKeydown} />

<div bind:this={host}>
  <button
    type="button"
    class="drawer-backdrop"
    tabindex="-1"
    aria-label={$dict.common.close}
    on:click={onclose}
  ></button>
  <div
    class="drawer"
    role="dialog"
    aria-modal="true"
    aria-labelledby="drawer-title"
    tabindex="-1"
    bind:this={panel}
  >
    <div class="drawer-head">
      <h2 class="card-title" id="drawer-title">{title}</h2>
      <button type="button" class="btn btn-ghost btn-sm" aria-label={$dict.common.close} on:click={onclose}>
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
          <path d="M18 6 6 18M6 6l12 12" />
        </svg>
      </button>
    </div>
    <div class="drawer-body">
      <slot />
    </div>
  </div>
</div>
