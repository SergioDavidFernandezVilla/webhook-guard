---
name: webhook-guard dashboard
description: Read-only observability for an idempotent webhook ledger, drawn as an approach-control strip rack.
colors:
  rack: "#0f1415"
  bay: "#171d1f"
  bay-2: "#1e2629"
  bay-3: "#131a1c"
  slip: "#e9dfc8"
  slip-2: "#ded1b5"
  slip-edge: "#b9a986"
  press: "#1d211d"
  press-2: "#39413a"
  press-muted: "#5d675f"
  chalk: "#e8ede9"
  chalk-2: "#b8c3bd"
  chalk-muted: "#8c9a94"
  rule: "#293234"
  rule-strong: "#3a474a"
  signal-green: "#43b979"
  signal-green-soft: "rgba(67, 185, 121, 0.14)"
  signal-green-ink: "#0f5a38"
  signal-amber: "#e0a63a"
  signal-amber-soft: "rgba(224, 166, 58, 0.14)"
  signal-amber-ink: "#6b440a"
  amber-fill: "#e0a63a"
  signal-red: "#e0564f"
  signal-red-soft: "rgba(224, 86, 79, 0.14)"
  signal-red-ink: "#7d1f16"
  signal-blue: "#6f9fd8"
  signal-blue-soft: "rgba(111, 159, 216, 0.14)"
  signal-blue-ink: "#234a6e"
  signal-violet: "#b08bd6"
  signal-violet-soft: "rgba(176, 139, 214, 0.14)"
  signal-violet-ink: "#543a6d"
  topbar: "#0b0f10"
  day-rack: "#dcded7"
  day-bay: "#f4f4ef"
  day-slip: "#f3ead6"
typography:
  display:
    fontFamily: "Archivo Variable, ui-sans-serif, system-ui, sans-serif"
    fontSize: "2.05rem"
    fontWeight: 650
    lineHeight: 1.03
    letterSpacing: "-0.035em"
  body:
    fontFamily: "Archivo Variable, ui-sans-serif, system-ui, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: "0em"
  label:
    fontFamily: "Archivo Narrow Variable, Archivo Variable, ui-sans-serif, sans-serif"
    fontSize: "0.64rem"
    fontWeight: 650
    lineHeight: 1.2
    letterSpacing: "0.1em"
  data:
    fontFamily: "Azeret Mono Variable, ui-monospace, SFMono-Regular, Consolas, monospace"
    fontSize: "0.72rem"
    fontWeight: 400
    lineHeight: 1.4
    letterSpacing: "0em"
rounded:
  sm: "2px"
  md: "4px"
  pill: "999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "24px"
  xl: "32px"
components:
  button-primary:
    backgroundColor: "{colors.amber-fill}"
    textColor: "#17130a"
    rounded: "{rounded.sm}"
    padding: "6px 10px"
  button-default:
    backgroundColor: "{colors.slip}"
    textColor: "{colors.press}"
    rounded: "{rounded.sm}"
    padding: "6px 10px"
  badge-success:
    backgroundColor: "{colors.signal-green-soft}"
    textColor: "{colors.signal-green}"
    typography: "{typography.label}"
    rounded: "{rounded.sm}"
  badge-warning:
    backgroundColor: "{colors.signal-amber-soft}"
    textColor: "{colors.signal-amber}"
    typography: "{typography.label}"
    rounded: "{rounded.sm}"
  card:
    backgroundColor: "{colors.bay}"
    rounded: "{rounded.md}"
  input:
    backgroundColor: "{colors.bay-2}"
    textColor: "{colors.chalk}"
    rounded: "{rounded.sm}"
    padding: "6px 9px"
---

# Design System: webhook-guard dashboard

## Overview

**Creative North Star: "The Strip Rack"**

The dashboard is an approach-control strip bay, not a SaaS dashboard. Every webhook arrives as a paper strip, is filed in a bay in time order, and keeps its identity while its state is stamped in place. Late deliveries sit in the late bay; reconciliation is a clearance sheet. The surface is a dark rack of charcoal bays with manila paper strips; the reader's eye lands on the paper, not on chrome. The world enters through type, palette, density and one signature move; navigation and controls stay standard web controls, because the operator came to read state.

The composition is a rack: a dark top utility bar, a status band of four manila slips, a full-width event strip board, a recessed bay rail for system health, the last clearance, charges by state and late events, then the activity chart. The memorable moment is the punched revision chain: `created → authorized → captured` as filled cells, the current cell ticked, future cells dashed, a rejected cell struck. State stamps state its meaning in words.

**Key Characteristics:**
- Rack charcoal ground with manila strips as the data surface; the paper carries the evidence, the chrome stays quiet.
- Signal green, amber, red and violet only where they carry domain state.
- Archivo for reading and UI, Archivo Narrow for bay labels, Azeret Mono for IDs, timestamps and amounts.
- Flat surfaces separated by hairlines and 4px rack gaps; no decorative shadows.
- Standard top-bar navigation and controls; no permanent sidebar, no terminal costume.
- The revision chain and the clearance stamp are the product's signatures and must carry state, never decoration.

## Colors

A restrained product palette built on two materials: charcoal rack and manila paper. Dark is the canonical scene (a control room at night); light is the day shift on the same rack.

### Primary
- **Signal Amber** (#e0a63a): the one affirmative action, the pending state and the current tick. In the day shift it darkens to #8a5a10 for text contrast.
- **Signal Green** (#43b979): applied, cleared, captured. On paper, text uses **Signal Green Ink** (#0f5a38).
- **Signal Red** (#e0564f): rejected, failed, a clearance with differences. On paper, text uses **Signal Red Ink** (#7d1f16).
- **Signal Blue** (#6f9fd8): authorized and healthy in-flight state; never the warning tone.
- **Signal Violet** (#b08bd6): refunded.

### Neutral
- **Rack** (#0f1415 dark, #dcded7 day): page ground.
- **Bay** (#171d1f dark, #f4f4ef day): cards and boards.
- **Bay Recess** (#1e2629 dark, #e6e7df day): rail, table hover, inputs.
- **Strip** (#e9dfc8 dark, #f3ead6 day): the manila surface that carries data.
- **Strip Shade** (#ded1b5) and **Strip Edge** (#b9a986): strip hover and the paper edge.
- **Press Ink** (#1d211d), **Press Secondary** (#39413a) and **Press Muted** (#5d675f): text on paper.
- **Chalk** (#e8ede9), **Chalk Secondary** (#b8c3bd) and **Chalk Muted** (#8c9a94): text on the rack.
- **Hairline** (#293234) and **Hairline Strong** (#3a474a): all rules on the rack.
- **Topbar Graphite** (#0b0f10): the utility bar in both themes.

**The Signal Is State Rule.** Signal colors mark domain state, the affirmative action, or the current revision. They never fill a card, a decorative edge, or a hover wash.

**The Paper Carries Data Rule.** Manila is the data surface. Do not use it for decoration, page chrome or a second card layer.

**The Press Ink Rule.** Text and links on a manila surface use Press Ink, never Chalk. Chalk is for the rack only.

**The State Is Text Rule.** A stamp always states its meaning in words. Color may reinforce, never replace, the label.

## Typography

**Display / UI Font:** Archivo Variable (ui-sans-serif, system-ui)
**Bay Label Font:** Archivo Narrow Variable (Archivo Variable fallback)
**Data Font:** Azeret Mono Variable (ui-monospace)

**Character:** A neutral grotesk for reading and operating, a condensed grotesk for the short bay labels, and a measured mono for evidence. The contrast is functional: prose is sans, data is mono, labels are narrow caps.

### Hierarchy
- **Display** (650, 1.6–2.05rem, 1.03, -0.035em): page titles only.
- **Title** (650, 0.88rem, 1.3): card and drawer titles.
- **Body** (400, 13px, 1.5): descriptions, table cells, rail copy; measure stays under 70ch.
- **Label** (650, 0.64rem, 0.1em, uppercase, Archivo Narrow): column headers, bay labels, rail labels.
- **Data** (400, 0.72rem, Azeret Mono): IDs, timestamps, amounts, counts, codes.

**The Monospace Is Measurement Rule.** Azeret Mono is for IDs, timestamps, amounts, counts, codes and the minute table. It is never body copy, a heading, a button, or a costume for "technical".

## Layout

A full-width shell: a 3.5rem sticky utility bar, then a page capped at 96rem with 1.75rem–2.25rem side padding. The first viewport is a status band of four manila slips over a 1.55fr / 1fr grid: the event strip board on the left and the bay rail on the right; the activity chart follows full width. Reconciliation puts the clearance sheet full width, then the differences, then the history. Charges and Events keep one full-width strip board with the filter row inside its header.

Breakpoints: 1279px wraps the utility bar into brand/tools then a tab row; 1100px collapses the 2-column grid; 1023px swaps strip boards for slip lists; 767px stacks the page header, drops the status band to two columns and makes the drawer full width; 640px hides the brand tagline.

The spacing rhythm is 4/8: 4px between strips in a rack, 8px between related controls, 16px between card sections, 24–32px between page blocks.

## Elevation & Depth

Flat by default. Depth is conveyed by material changes and 1px hairlines: the rack is the ground, the bay is one step up, the manila strip is the lit paper in the rack, and the drawer dims the shell with a flat scrim and a 1px rule. There is no ambient shadow vocabulary.

**The Flat-By-Default Rule.** If a border or a rack gap can separate two surfaces, no shadow is allowed. Focus is the only state that may paint outside the line, and it uses a 2px signal ring with a 2px offset.

## Shapes

Squarely operational: 2px radius for strips, buttons, stamps and inputs; 4px for bays and the drawer; 999px only for the live-status lamp. Borders are 1px. The strip board separates rows with 4px rack gaps and a 2px radius at each strip's ends. No pills, no nested cards, no one-sided colored bars; a callout may carry a full border and a tint because it marks a real warning or error.

## Components

### Buttons
- **Shape:** 2px radius, 32px minimum height, 6px 10px padding.
- **Primary:** Signal Amber fill with near-black text; hover lightens. This is the single affirmative action on a surface.
- **Default:** manila fill, strip edge, press ink; hover strip shade. On a manila strip, a default button is one shade darker so it separates from the paper.
- **Ghost:** transparent until hover, used for inline copy and clear actions; on paper it uses press ink, on the rack it uses chalk.
- **Focus:** 2px signal ring with 2px offset; on paper the ring switches to press ink.

### Stamps
- **Style:** 2px radius, mono label at 0.56rem, uppercase, 0.06em tracking, soft state tint and a filled square mark.
- **State:** a filled square plus a word; authorized is blue, captured is green, pending is amber, failed is red, refunded is violet, created is neutral.
- **On paper:** the word uses the dark ink variant of the state, so it stays readable against manila.

### Bays / Cards
- **Corner Style:** 4px.
- **Background:** bay; the rail uses the recess.
- **Shadow Strategy:** none; see Elevation.
- **Border:** 1px hairline.
- **Internal Padding:** 16px; 12px in tight card headers.

### Strip Board (tables)
- **Style:** a dark board with a 1px strong rule under the header; each row is a manila strip separated by a 4px rack gap, with a dashed right edge on the revision tab and a 2px radius at the strip's ends.
- **Text:** press ink on paper; tabular numerals; IDs and timestamps in mono; row hover steps to strip shade.
- **Mobile:** below 1023px the board becomes a list of manila slips with the same data and a labeled revision chain.

### Revision Chain (signature)
A row of punched cells under a revision code. Done and current cells are filled with the state signal; the current cell carries a 2px tick above it; future cells are dashed with a small slot; a rejected cell is struck through; refunded is violet. The labeled variant puts the state names under the cells in Archivo Narrow. It is `role="img"` with a full sentence in `aria-label`, including the current state for a charge.

### Clearance Stamp (signature)
The reconciliation result as a rotated 2px-bordered stamp on the manila clearance sheet: `NO DIFFERENCES · 3,690 CHARGES CHECKED` or `DIFFERENCES FOUND · 3,670 DIFFERENCES`. The stamp is the one place a rotation is allowed.

### Inputs / Fields
- **Style:** recess fill, 1px strong rule, 2px radius, 32px height.
- **Focus:** 2px signal ring with 2px offset; the field's border also takes the signal.
- **Placeholder:** chalk muted; select keeps the native control.

### Navigation
- **Style:** a dark top bar with a strip mark, a paper wordmark, a mono tagline, six bay tabs, language and theme toggles, and a Local lamp.
- **Default / hover:** topbar muted; hover raises contrast with a translucent white wash.
- **Active:** a manila strip with press text in both themes; the active tab is the one strip pulled from the rack.
- **Mobile:** brand and tools on row one, then the tabs wrap onto as many rows as needed; nothing is clipped and no tab is hidden off-canvas.

### Drawer
A right-side dossier: a manila header strip, a dark bay body, a flat scrim, a focus trap, `inert` on the shell and the skip link, and the previous focus restored on close. The body uses slip lists on narrow screens instead of horizontal tables.

## Do's and Don'ts

### Do:
- **Do** keep data on manila strips and chrome on the rack.
- **Do** draw the revision chain as punched cells: fill done and current, dash future, strike rejected.
- **Do** keep every figure in tabular numerals and every ID in Azeret Mono.
- **Do** state every state in words on a stamp; color is reinforcement.
- **Do** put filters inside the header of the card they filter.
- **Do** let the bay rail hold system health, last clearance, charges by state and late events.
- **Do** test both EN/ES and light/dark at 1920, 1024, 900, 768 and 390.
- **Do** announce counters, pagination and copy feedback with `aria-live`.
- **Do** trap focus in the drawer and set the shell and the skip link `inert` while it is open.

### Don't:
- **Don't** add a permanent sidebar; navigation lives in the top utility bar.
- **Don't** use signal colors as decoration, a second accent or a hover wash.
- **Don't** use monospace for prose, headings or buttons.
- **Don't** use shadows, glass, gradient text or one-sided colored bars.
- **Don't** paint the healthy `authorized` state with the warning tone; use blue.
- **Don't** put Chalk text or links on manila; use Press Ink.
- **Don't** replace the strip board with a standard white table, or the chain with a progress bar.
- **Don't** promote the Demo tab while `DEMO_MODE` is off.
- **Don't** add a second display face or a new material without a documented reason.
