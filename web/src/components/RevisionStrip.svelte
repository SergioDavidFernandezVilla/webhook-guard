<script lang="ts">
  import { dict } from "../i18n";

  export let state = "";
  export let kind: "charge" | "event" = "charge";
  export let status: "applied" | "pending" | "rejected" | "" = "";
  export let labeled = false;

  const CHARGE_PATHS: Record<string, string[]> = {
    created: ["created", "authorized", "captured"],
    authorized: ["created", "authorized", "captured"],
    captured: ["created", "authorized", "captured"],
    failed: ["created", "authorized", "failed"],
    refunded: ["created", "authorized", "captured", "refunded"],
  };

  const EVENT_STEPS: Record<string, string[]> = {
    "payment.created": ["created"],
    "payment.authorized": ["created", "authorized"],
    "payment.captured": ["created", "authorized", "captured"],
    "payment.failed": ["created", "authorized", "failed"],
    "payment.refunded": ["created", "authorized", "captured", "refunded"],
  };

  $: steps =
    kind === "event"
      ? (EVENT_STEPS[state] ?? ["created", "authorized", "captured"])
      : (CHARGE_PATHS[state] ?? ["created", "authorized", "captured"]);
  $: currentIndex =
    kind === "event"
      ? steps.length - 1
      : Math.max(0, steps.indexOf(state));
  $: terminal = steps[currentIndex] ?? "";
  $: statusLabel =
    status === "applied"
      ? $dict.common.applied
      : status === "pending"
        ? $dict.common.pending
        : status === "rejected"
          ? $dict.common.rejected
          : "";
  $: currentLabel = kind === "charge" ? ($dict.states[state] ?? state) : "";
  $: ariaLabel = `${$dict.common.revision}: ${steps.map((step) => $dict.states[step] ?? step).join(" → ")}${currentLabel ? ` · ${currentLabel}` : ""}${statusLabel ? ` · ${statusLabel}` : ""}`;

  function segmentStatus(index: number): string {
    if (index < currentIndex) return "done";
    if (index > currentIndex) return "future";
    if (status === "rejected") return "rejected";
    if (status === "pending") return "pending";
    if (kind === "charge") {
      if (state === "failed") return "failed";
      if (state === "refunded") return "refunded";
      return "done";
    }
    return terminal === "failed" ? "failed" : terminal === "refunded" ? "refunded" : "done";
  }
</script>

<span class="chain" class:chain-labeled={labeled} role="img" aria-label={ariaLabel} style={`--steps:${steps.length}`}>
  <span class="chain-track" aria-hidden="true">
    {#each steps as step, index}
      <span
        class="chain-notch"
        data-status={segmentStatus(index)}
        data-current={index === currentIndex}
      >
        <span class="chain-slot"></span>
      </span>
    {/each}
  </span>
  {#if labeled}
    <span class="chain-labels" aria-hidden="true">
      {#each steps as step, index}
        <span class="chain-label" data-status={segmentStatus(index)} data-current={index === currentIndex}>
          {$dict.states[step] ?? step}
        </span>
      {/each}
    </span>
  {/if}
</span>

<style>
  .chain {
    display: inline-flex;
    align-items: center;
    min-width: 4.2rem;
    vertical-align: middle;
  }

  .chain-labeled {
    flex-direction: column;
    align-items: stretch;
    gap: 0.3rem;
    width: 100%;
  }

  .chain-track {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 3px;
  }

  .chain-track::before {
    content: "";
    position: absolute;
    left: 0;
    right: 0;
    top: 50%;
    height: 1px;
    background: color-mix(in srgb, currentColor 26%, transparent);
  }

  .chain-labeled .chain-track {
    display: grid;
    grid-template-columns: repeat(var(--steps, 3), minmax(0, 1fr));
    width: 100%;
  }

  .chain-notch {
    position: relative;
    z-index: 1;
    display: block;
    width: 0.95rem;
    height: 0.68rem;
    border: 1px solid color-mix(in srgb, currentColor 55%, transparent);
    border-radius: 2px;
    background: var(--chain-bg, transparent);
  }

  .chain-labeled .chain-notch {
    width: auto;
    height: 0.7rem;
  }

  .chain-notch::after {
    content: "";
    position: absolute;
    left: 50%;
    top: -5px;
    width: 2px;
    height: 3px;
    transform: translateX(-50%);
    background: transparent;
  }

  .chain-notch[data-current="true"]::after {
    background: currentColor;
  }

  .chain-slot {
    position: absolute;
    left: 50%;
    top: 50%;
    width: 0.38rem;
    height: 0.18rem;
    transform: translate(-50%, -50%);
    background: transparent;
  }

  .chain-notch[data-status="done"] {
    border-color: var(--signal-green);
    color: var(--signal-green);
    background: var(--signal-green);
  }

  .chain-notch[data-status="done"] .chain-slot {
    background: var(--chain-bg, transparent);
  }

  .chain-notch[data-status="pending"] {
    border-color: var(--signal-amber);
    color: var(--signal-amber);
    background: var(--signal-amber);
  }

  .chain-notch[data-status="pending"] .chain-slot {
    background: var(--chain-bg, transparent);
  }

  .chain-notch[data-status="rejected"],
  .chain-notch[data-status="failed"] {
    border-color: var(--signal-red);
    color: var(--signal-red);
    background: var(--signal-red);
  }

  .chain-notch[data-status="rejected"] .chain-slot,
  .chain-notch[data-status="failed"] .chain-slot {
    background: var(--chain-bg, transparent);
  }

  .chain-notch[data-status="rejected"]::before {
    content: "";
    position: absolute;
    left: -1px;
    right: -1px;
    top: 50%;
    height: 1px;
    background: var(--chain-bg, currentColor);
    transform: rotate(-32deg);
  }

  .chain-notch[data-status="refunded"] {
    border-color: var(--signal-violet);
    color: var(--signal-violet);
    background: var(--signal-violet);
  }

  .chain-notch[data-status="refunded"] .chain-slot {
    background: var(--chain-bg, transparent);
  }

  .chain-notch[data-status="future"] {
    border-style: dashed;
  }

  .chain-notch[data-status="future"] .chain-slot {
    width: 0.3rem;
    height: 1px;
    background: color-mix(in srgb, currentColor 32%, transparent);
  }

  .chain-labels {
    display: grid;
    grid-template-columns: repeat(var(--steps, 3), minmax(0, 1fr));
    gap: 3px;
  }

  .chain-label {
    color: inherit;
    font-family: var(--font-narrow);
    font-size: 0.62rem;
    font-weight: 600;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    opacity: 0.62;
  }

  .chain-label[data-status="done"],
  .chain-label[data-status="pending"],
  .chain-label[data-status="refunded"] {
    opacity: 1;
  }

  .chain-label[data-current="true"] {
    font-weight: 700;
  }

  .chain-label[data-status="rejected"] {
    opacity: 1;
    text-decoration: line-through;
  }

  :global(.strip-row) .chain-label,
  :global(.card-list-item) .chain-label {
    color: var(--press-2);
  }

  :global(.strip-row) .chain-label[data-status="rejected"],
  :global(.card-list-item) .chain-label[data-status="rejected"] {
    color: var(--signal-red-ink);
  }

  @media (max-width: 380px) {
    .chain-label {
      font-size: 0.54rem;
      letter-spacing: 0.02em;
    }
  }
</style>
