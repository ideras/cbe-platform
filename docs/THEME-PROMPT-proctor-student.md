# Implementation prompt: warm palette for the proctor and student apps

**Reference:** the calculator mockup `docs/exam-calculator-redesign.html`. Its `:root` block holds twelve of the color tokens used here. Its `--display` is `--surface-muted` here, with the same value. `--ocean-strong`, `--sand-ink`, `--ok`, `--ok-on-dark` and the three `--sidebar` tokens are new in this prompt.

## 1. Problem

The proctor dashboard uses too much blue. The navy sidebar, the bright blue active item, the blue primary and outlined buttons, the blue tab underline, the blue headings and badges, and the blue row-action icons all compete. The goal is a neutral warm base where blue marks only the primary action, the active tab, and the focus ring. Other roles get their own colors: sea for info and enabled features, sand for neutral highlights, coral for danger, and a green status dot.

## 2. Scope

- **In scope:** colors, borders, background fills, and text colors in the proctor app and the student app. This includes the calculator (`shared/theme/calculator/`), which the student app shows during the exam and the proctor shows in its exam preview, and the student's code editor (CodeMirror, `apps/student/frontend/src/CodeEditor.svelte`).
- **Out of scope:** layout, sizes, spacing, fonts, copy, icon shapes, behavior, routing, and fullscreen or focus-loss code. Do not add or remove components. The mockup's IBM Plex fonts are not adopted: the theme ships no remote assets.
- **Not in this change:** dark mode. The tokens are named so a dark theme can follow later.

## 3. Tokens

Both apps already share one theme file, `shared/theme/ocean-blue.css`, imported by each app's `frontend/src/main.js`. The proctor adds its own color tokens and literals in `apps/proctor/frontend/src/dashboard.css` (`--brand*`, `--nav-*`, `--page-bg`, `--panel-border`, `--title-ink`). After this change, the shared theme file holds every color token, including the proctor's, and `dashboard.css` only uses them. Rename the file to match the palette (proposed: `shared/theme/warm.css`), and update both imports.

Most colors are already tokens (`--color-*`, `--ocean-*`, `--brand*`, `--nav-*`), so most of the work is mapping those tokens to the ones below, not replacing literals.

| Token | Value | Role |
|-------|-------|------|
| `--ground` | `#EFEBE4` | Page background |
| `--surface` | `#FFFFFF` | Cards, tables, inputs |
| `--surface-muted` | `#F6F3EC` | Table headers, neutral banners |
| `--ink` | `#1D2B33` | Primary text |
| `--ink-2` | `#56656E` | Secondary text, ids, idle icons, control borders |
| `--line` | `#DCD5C8` | Decorative borders and dividers |
| `--ocean` | `#1F5F8B` | Primary button, active tab underline, focus ring. Nothing else. |
| `--ocean-strong` | `#174A6C` | Hover on primary button |
| `--sea` | `#DFF0EB` | Info and success tint, enabled-feature tags |
| `--sea-ink` | `#1E5E58` | Text and dashed border on sea |
| `--sand` | `#F1E7D2` | Neutral highlight, pending states |
| `--sand-ink` | `#6B4E1E` | Text on sand |
| `--coral` | `#B0452C` | Danger text and border |
| `--coral-tint` | `#F8E2DB` | Danger hover background |
| `--ok` | `#2F7D5C` | Status dot on a light background only. Always paired with a text label. |
| `--ok-on-dark` | `#3F9C76` (proposed) | Status dot on the sidebar. `--ok` reaches only 2.9:1 on `--sidebar` and 2.4:1 on the sidebar's status box. This value reaches 4.3:1 and 3.5:1. |
| `--sidebar` | `#1D2B33` | Sidebar background (replaces the navy) |
| `--sidebar-ink` | `#EFEBE4` | Sidebar text, and the focus ring inside the sidebar |
| `--sidebar-raised` | `#2B3E4A` | Active sidebar item background |

## 4. Mapping for the proctor dashboard (proposal, confirm in step 1)

The elements below are on the class detail page (Classes, then a class, Exams tab) and in the sidebar.

| Element | Now | Becomes |
|---------|-----|---------|
| Sidebar background | navy gradient | `--sidebar` |
| Sidebar item text | white | `--sidebar-ink` |
| Active sidebar item (Classes) | bright blue, cyan edge | `--sidebar-raised`, text unchanged |
| Connected dot in the sidebar footer | green | `--ok-on-dark` dot, text stays `--sidebar-ink` |
| Other connection dots (connecting, lost, failed) | amber, red | propose in step 1, at least 3:1 on the status box |
| Focus ring inside the sidebar | blue | `--sidebar-ink`. `--ocean` reaches only 2.1:1 on `--sidebar` and 1.6:1 on `--sidebar-raised`. |
| "Admission closed" banner | grey | `--surface-muted` background, `--ink-2` text and dot |
| "← Classes" button | outlined blue | outlined `--ink` (border `--ink-2`) |
| "Rename…" button | outlined blue | outlined `--ink` (border `--ink-2`) |
| "Delete class…" button | outlined red | `--coral` text and border, `--coral-tint` on hover |
| Page title "Test Class" | navy | `--ink` |
| Tab "Exams (2)" when active | blue | `--ink` text, `--ocean` underline |
| Tab "Students (2)" when inactive | grey | `--ink-2` |
| "Import exam…" button | blue fill | `--ocean` fill, `--ocean-strong` on hover |
| Status line after an action ("Exam … deleted.") | light blue | `--sea` background, `--sea-ink` text |
| Exam titles (bold text, not links) | navy | `--ink`, bold |
| "Calculator: scientific · selectable angle" badge | blue dashed | `--sea` background, `--sea-ink` text, dashed `--sea-ink` border (keep the dash: it marks an optional feature) |
| "Published", "Monitoring" and "Forward only" badges | blue and status colors | propose in step 1, by meaning |
| Exam ids under the titles | grey | `--ink-2` |
| Table header row | pale blue-grey | `--surface-muted`, text `--ink-2` |
| Table rows and card border | grey | `--line` |
| Row-action buttons: their border and hover | blue | `--line` border, `--surface-muted` on hover |
| Row-action icons: Monitor, Preview, Publish, Close admission, Download submissions, Purge screenshots, Delete exam | multicolor illustrations | see section 8: these are SVG files with their own colors |
| Focus rings (all controls outside the sidebar) | blue | `--ocean` |

**Any other blue in the code:** report it with the file and line. Map it by meaning, not by appearance: info goes to sea, neutral emphasis to sand or ink, danger to coral. Do not keep a blue because it looks right. Two cases to look for:

- **Text selection** (`::selection` in the theme, and the CodeMirror selection) is a pale blue today.
- **The focus-loss color** (`--color-focus`, a purple) is not the focus ring. It marks a student's focus-loss events in the Monitoring view and the review window. It is a status color (rule 5): map it to a status role, never to `--ocean`.

**Student app:** section 4 maps only the proctor. Step 1 proposes the student mapping too. It covers the login screen, the exam page with its bands and dialogs, the code editor, the lock screen, the focus-loss warning, and the calculator. The calculator follows the mockup.

## 5. Rules

1. **Ocean is limited.** It appears only on the primary button, the active tab underline, and focus rings outside the sidebar. In the calculator, it also appears on the selected mode and the `=` key, as in the mockup. Use at most one primary button per view. Do not use ocean for links, badges, icons, borders, or charts.
2. **No raw colors.** After the change, no color literal remains outside the theme file, except in the icon SVG files until section 8's decision. The apps use no Tailwind or component-library palette.
3. **Contrast.** Body and secondary text reach 4.5:1 on their background. Borders and icons that carry meaning reach 3:1, and so do the borders of inputs and other controls: use `--ink-2` for them, not `--line` (1.5:1 on `--surface`). Write a frontend test that computes the ratio for each pair used in section 4 and fails below the limit.
4. **Color is never the only signal.** Keep the text label beside the status dot. Keep the dashed border on the feature badge. Keep the danger wording on coral buttons.
5. **Semantic colors stay semantic.** Green, red, yellow, and the focus-loss purple appear only where they mean status. Do not use them for decoration.
6. **Charts.** The dashboard has no charts today. If one is added before this change lands, report its series colors in step 1 and do not change them until I confirm a series palette, proposed as ink, sea, sand, coral, and a lighter ink tint.
7. **Tests.** Existing tests pass. A few UI tests assert a computed color, for example the page background and a warning row in `apps/proctor/frontend/tests/ui/dashboard.spec.js`, a border in `class-exams.spec.js`, and the page, input and editor colors in `apps/student/frontend/tests/ui/student.spec.js` and `capture-access.spec.js`. Update only those expected values, to the new tokens. There are no screenshot snapshot tests.

## 6. Steps

1. **Inventory and mapping. Stop and report.** Search both apps (`apps/proctor/frontend/src`, `apps/student/frontend/src`) and `shared/theme` for hex, rgb, hsl, and CSS variables, and list the colors inside `apps/proctor/frontend/src/icons/*.svg`. Group results by file and by role. Give the proposed token for each existing token and literal. Flag anything the mapping does not cover.
2. **After I confirm:** rename the theme file, move the proctor's color tokens into it, set the new tokens, and update both imports. Commit that alone.
3. Replace the remaining tokens and literals, one app per commit. The shared calculator goes with the student app. Apply section 5 as you go.
4. Add the contrast test and a check that fails on raw color literals outside the theme file. Make them node tests (`tests/*.test.js`), so `npm test`, and with it `make student-ui-test` and `make proctor-ui-test`, run them. The project has no CI.
5. **Visual pass.** Take before and after screenshots of: the proctor Classes page, a class with exams, the Monitoring window, the proctor's exam preview with the calculator, and the student exam page with the calculator open in scientific and programmer modes. Count the blue elements in each pair. The after count should be the ocean elements only.

## 7. Acceptance

Owner-approved amendments and verification evidence: [decisions](theme/DECISIONS.md),
[inventory](THEME-INVENTORY-proctor-student.md), [implementation report](theme/REPORT.md).
Meaningful control borders use ink-2, and eight proctor action icons use local
inline Lucide SVGs (Close admission: door-closed-locked). Their image-specific test
was adapted to SVG while preserving its sizing/accessibility checks.

- [x] Inventory report approved by the owner before any code changes.
- [x] The theme file is the only place color values are defined in both apps, apart from the icon SVGs as decided in section 8.
- [x] Ocean appears only where rule 1 allows it, in every screenshot.
- [x] Contrast test passes for every pair in section 4.
- [x] No layout, copy, or behavior changed. Existing tests pass, with only the color assertions of rule 7 updated (plus the owner-approved icon/SVG test amendment above).
- [x] Calculator in the student app and in the proctor's exam preview uses the same color tokens as the calculator mockup.
- [x] Fullscreen, focus-loss detection, and the focus-loss alarm are untouched.

## 8. Stop and report if

- **The row-action icons.** The proctor's row-action icons are multicolor SVG files in `apps/proctor/frontend/src/icons/`, loaded as images, so CSS tokens cannot recolor them. Monitor and Publish are drawn in the old Ocean Blue palette, and the others mix many colors. Propose one approach in step 1, for example redrawing them in the new palette, or as one-color icons that follow `currentColor` (a change of the files, not of the icon shapes). Wait for a decision.
- A third-party component brings its own theme and cannot be overridden through tokens. The only one today is CodeMirror, which already takes its colors from the theme's CSS variables.
- The inventory finds colors that change meaning in more than one place, for example a status color reused for a button. List them for a decision.
