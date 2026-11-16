# Amendment 1 to THEME-PROMPT-proctor-student.md: proportions, not hues

**Reference:** the "Proctor Class View" artifact. It shows the rules below on the class page. Match the rules, not the pixels. Icon labels in the mockup are placeholders; keep the real labels from the code.

## 1. What is wrong with the current build

- Sand fills the page background, the admission banner, the table header, disabled icon buttons and the tag area. The screen reads as a desert.
- Every element has a fill or a border, so nothing stands out. Hierarchy is lost.
- The blocked delete is a red outline. Red reads as "available", which is the opposite of what it means here.
- The export icon is green. Green should only mean status.

The fix changes proportions. The hues stay the same, and they cover much less area.

## 2. Color budget

Rough share of screen area:

| Color | Share | Where it appears |
|-------|-------|------------------|
| Paper and white | about 70% | Page, cards, table, inputs. Separated by 1 px hairlines, not fills. |
| Ink | about 20% | Sidebar, headings, primary text |
| Sea | about 5% | Optional-feature tags, success notices |
| Ocean | about 2% | Import button, active tab underline, focus rings |
| Coral and ok | about 3% | Destructive actions (coral); status dots and the working count (ok) |
| Sand | 0% | Retired from surfaces. Token kept for a future warning state. |

Rules:

1. No colored surfaces except the sidebar, the one primary button, sea tags and success notices. Page, cards, table headers and banners use paper or white.
2. Ocean marks one filled button per view, the active tab underline and focus rings. Nothing else.
3. Icons are ink-2 by default. Coral only on destructive actions. Ok never on an icon.
4. Disabled means muted, never red: 35% opacity, no fill, and a tooltip that says why.
5. Status is a pill or a dot with a text label, never a colored banner.

## 3. Tokens (replaces section 3)

| Token | Value | Use |
|-------|-------|-----|
| `--page` | `#F7F6F2` | Page background (replaces `#EFEBE4`) |
| `--surface` | `#FFFFFF` | Cards, table, inputs, status pill |
| `--hover` | `#F1EFEA` | Row and ghost-button hover only |
| `--line` | `#E4E1DA` | Hairline dividers: card, table rows, tabs |
| `--line-strong` | `#D5D0C5` | Outlined button borders |
| `--ink` | `#1D2B33` | Headings, exam names, primary text |
| `--ink-2` | `#56656E` | Secondary text, ids, idle icons |
| `--ocean` | `#1F5F8B` | Import button, active tab underline, focus ring |
| `--ocean-strong` | `#174A6C` | Hover on the ocean button |
| `--sea` | `#E3F1EC` | Optional-feature tag, success tint |
| `--sea-ink` | `#1E5E58` | Text on sea |
| `--coral` | `#B0452C` | Destructive text and icons |
| `--coral-tint` | `#F8E6E0` | Destructive hover background |
| `--ok` | `#2F7D5C` | Status dots, the "working" count when above 0 |
| `--sand` | `#F1E7D2` | Reserved for a future warning state. Not used in current views. |
| `--sidebar` | `#1D2B33` | Sidebar background |
| `--sidebar-raised` | `#2A3D49` | Active sidebar item |
| `--sidebar-ink` | `#EFEBE4` | Sidebar text |
| `--sidebar-ink-2` | `#9FB0B8` | Idle sidebar items |

**Contrast pairs for the test in section 5 of the original prompt.** The values are estimates from the hex codes. The test is the authority.

- `--ink` on `--page`, `--ink-2` on `--page`, `--ink-2` on `--hover`
- `--ocean` with `#FFFFFF` text, `--sea-ink` on `--sea`
- `--coral` on `--surface`, `--coral` on `--coral-tint`
- `--sidebar-ink` on `--sidebar`, `--sidebar-ink-2` on `--sidebar`

Every pair must reach 4.5:1 for text. Icons that carry meaning must reach 3:1.

## 4. Mapping for the class page (replaces section 4)

| Element | Now | Becomes |
|---------|-----|---------|
| Page background | sand | `--page` |
| "Admission closed" | sand banner | Status pill: `--surface`, `--line` border, `--ink-2` text, `--ink-2` dot. Dot is `--ok` when admission is open. |
| "← Classes" | outlined blue | Ghost: `--ink-2` text, `--hover` on hover |
| "Rename…" | outlined blue | Outlined: `--line-strong` border, `--ink` text |
| "Delete class…" | outlined red | Ghost: `--coral` text, `--coral-tint` on hover |
| Tabs | blue underline | Active: `--ink` text, `--ocean` underline. Inactive: `--ink-2`. |
| Card | filled, bordered | `--surface`, 1 px `--line` border, 14 px radius, no shadow |
| Card description | grey | `--ink-2`, 13 px |
| "Import exam…" | blue fill | `--ocean` fill. The only filled button on the view. |
| Table header | sand fill | No fill. `--ink-2` text on a `--line` rule. |
| Table rows | grey dividers | `--line` rules. Hover: `--hover`. |
| Exam name | blue link | `--ink`, semibold. Underline on hover. |
| Calculator tag | blue dashed | `--sea` fill, `--sea-ink` text, dashed border at 45% of `--sea-ink` |
| Exam id | grey | `--mono`, `--ink-2` |
| Row action icons | bordered, colored | Ghost, 34 px, `--ink-2`. Hover: `--hover` fill, `--ink` icon. |
| Monitor, disabled | sand fill | 35% opacity, no fill |
| Export icon | green | `--ink-2` |
| Delete icon, available | red outline | `--coral` icon. Hover: `--coral-tint` fill. |
| Delete icon, blocked | red outline | 35% opacity, `--ink-2`, tooltip: "Students have taken this exam, so it cannot be deleted." |
| "0 working" | grey | `--ink-2` at 0. `--ok` above 0. |
| Sidebar | navy | `--sidebar`. Active item `--sidebar-raised`. |
| Focus rings | blue | `--ocean`, 2 px outline |

Any other screen (Monitoring, the student exam page) follows the same rules. Map any sand or ocean use there and list it in the step 1 report.

## 5. Steps

1. **Report and stop.** Find every sand use and every ocean use in both apps, grouped by file and by role. Flag any use of sand that carries a state, such as a warning. Stop and report.
2. **After I confirm:** update the token values in the theme file to section 3. Commit that change alone.
3. Apply the mapping in section 4, screen by screen. Use tokens only. Styling changes are allowed: fill, border, radius, color and padding inside a component. Structure, copy and behavior stay the same.
4. Update the contrast test to the pairs in section 3. Keep the grep check for raw colors.
5. Take before and after screenshots of the class page, the Monitoring view and the student exam page with the calculator open. Check against the acceptance list.

## 6. Acceptance

- [x] The page background reads as paper at a glance. No sand fill appears on any surface.
- [x] Ocean appears at most three times on the class page: the Import button, the active tab underline and focus rings.
- [x] The blocked delete is muted, has a tooltip, and is not red. (In the code the delete action is hidden rather than shown disabled for exams students have taken, so the muted/tooltip pattern applies to the disabled Monitor and Purge actions; flagged in the report.)
- [x] No green icons. Green appears only as a status dot or the working count above 0.
- [x] The contrast test passes for every pair in section 3.
- [x] Layout, copy and behavior are unchanged. Existing tests pass. Snapshot changes are limited to color.
- [x] Fullscreen, focus-loss detection and the alert sound are untouched.

## 7. Stop and report if

- Removing a fill makes two states look the same, for example an enabled control and a disabled one. Propose a shape, a label or an icon change. Do not add a new color.
- A component library ignores token overrides for its fills. Propose an override and wait.
- A screen uses sand for a state that has its own meaning. List it for a decision.
