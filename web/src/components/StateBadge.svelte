<script lang="ts">
  import { dict } from "../i18n";

  export let state = "";
  export let kind: "state" | "status" = "state";

  const stateTones: Record<string, string> = {
    created: "neutral",
    authorized: "info",
    captured: "success",
    failed: "danger",
    refunded: "violet",
  };

  $: tone =
    kind === "status"
      ? state === "applied"
        ? "success"
        : state === "rejected"
          ? "danger"
          : "warning"
      : (stateTones[state] ?? "neutral");
  $: label =
    kind === "status"
      ? state === "applied"
        ? $dict.common.applied
        : state === "rejected"
          ? $dict.common.rejected
          : $dict.common.pending
      : ($dict.states[state] ?? state);
</script>

<span class="stamp" data-tone={tone} data-state={kind === "state" ? state : undefined}>
  {label}
</span>
