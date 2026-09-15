# gorbital — theme reference

Paste this into Claude Code as project context. Everything below is the
canonical visual system for gorbital: docs, landing page, README, CLI output.

---

## 1. Identity

Name:      gorbital — ALWAYS lowercase, one word.
           Never Gorbital, GORBITAL, Go-rbital or GOrbital.
           Lowercase even at the start of a sentence (rewrite the sentence
           instead of capitalising).
Spoken:    "gor-bit-al", like orbital with a g
Binary:    `orb`
Module:    gorbital.dev (import path); github.com/gorbital/gorbital

Tagline:       Go APIs, ready for orbit.
Short form:    Production Go APIs, from your repo.
Internal only: "Thin glue, thick library." (explains architecture to
               engineers; never put it on a landing page)

Meaning: gorbital is Go in orbit: an API that is launched complete, holds
its course, and stays in your hands. The mark is a ring cut by the
import-path slash, tilted 34°; the wordmark is Space Grotesk Bold,
lowercase, -4% tracking. Brand assets: docs/brand/logo.
---

## 2. Color

Dark theme is primary. Light theme is for README, docs and print only.

### Dark (primary)

| Token    | Hex       | Use                                                  |
|----------|-----------|------------------------------------------------------|
| ground   | `#0B0C0A` | Page background. Warm black, not blue.               |
| surface  | `#16180F` | Cards, panels. One step up from ground, never two.   |
| code-bg  | `#090A08` | Terminal blocks and code blocks. One step DOWN.      |
| accent   | `#C6F24A` | The board being taken. One use per screen.           |
| ink      | `#F2F1EC` | Headlines and primary body. 17:1 on ground.          |
| muted    | `#A8AB9F` | Secondary prose. The floor for body text.            |
| dim      | `#8E9285` | Labels, captions, inline comments in code.           |
| hairline | `#22251C` | 1px dividers. The ONLY divider. No shadows anywhere. |
| border   | `#2B2F23` | Card and panel borders.                              |
| border-2 | `#3B4031` | Secondary buttons, chips.                            |
| danger   | `#FF5C2B` | Errors, conflicts, DON'T callouts. Never pure red.   |

### Light (README, docs, print)

| Token         | Hex       |
|---------------|-----------|
| paper         | `#fbfaf6` |
| ink-dark      | `#14140f` |
| secondary     | `#57564f` |
| hairline      | `#c9c6bc` |

### Accent rules — important

- Lime is a FILL and a GROUND, not a text color.
- On light surfaces lime fails contrast outright. Never use it for text there.
- On dark it may be used for small mono labels, prompts, `$` signs, JSON keys
  and the mark — never for paragraphs.
- Text on a lime fill is always `#0B0C0A`.
- ONE accent moment per view. If two things are lime, neither is important.
- Never tint the wordmark accent.
- Max 1–2 background colors per page.

---

## 3. Type

Two families, both under the SIL Open Font License and always served by the
site or app itself, never fetched from Google at runtime (Next.js bundles them
with `next/font`; `modules/openapi/reference` embeds the variable woff2 files):

- **Manrope** (variable, 200–800) — headlines, UI, prose.
- **Geist Mono** (variable) — code, labels, data, terminal, all paths,
  payloads, headers, version numbers. Anything a developer would copy.

The logo lockup files in `logo/` were set in Space Grotesk before 2026-09-15
and stay as they are; live UI sets the wordmark in the site font.

Mono carries the technical register so the grotesque never has to shout.

### Scale

| Role    | Size                    | Weight | Tracking | Leading |
|---------|-------------------------|--------|----------|---------|
| Display | `clamp(40px,7vw,76px)`  | 700    | -0.05em  | 0.96    |
| H2      | `clamp(26px,3.6vw,36px)`| 700    | -0.04em  | 1.05    |
| Lead    | `clamp(22px,3vw,34px)`  | 500    | -0.02em  | 1.22    |
| Body    | 17px                    | 400    | normal   | 1.65    |
| Small   | 15px                    | 400    | normal   | 1.55    |
| Code    | 13px mono               | 400    | normal   | 1.8     |
| Label   | 12px mono, UPPERCASE    | 400    | 0.12em   | —       |

Headlines always run tight: -0.04em and under. Body copy caps at ~68ch.
Use `text-wrap: pretty` on prose.

---

## 4. Layout

- Max content width 1080px (1240px for wide reference tables).
- Sections separated by `1px solid #22251C` + 48–72px padding.
- Rounded corners, from one scale: 8px small controls and inputs, 12px
  cards, panels and code blocks, 16–24px dialogs and hero panels, fully round
  (999px) buttons, tabs, pills and search fields. The logo mark stays square.
- Depth comes from the 1px border and the surface step. Shadows only on
  floating layers (dialogs, menus); a soft lime glow (`rgba(198,242,74,.14)`)
  marks focus and the active "Try it" form.
- Section labels: `12px mono, 0.12em tracking, accent color`, numbered
  `01 /`, `02 /` …
- Grids: `repeat(auto-fit, minmax(280px, 1fr))` with `gap: 16–20px`.
- Always flex/grid + `gap`, never margin-spaced inline siblings.
- Dense data goes in bordered tables built from CSS grid rows with
  hairline dividers — not cards.

---

## 5. The mark

Four stacked bars: milled boards seen end-on. The top bar is pulled clear
to the RIGHT and struck in accent — the piece being taken to the bench.

```
Grid:           14 × 14 units
Bar:            14 wide × 3 tall
Gap:            2/3 unit
Top bar offset: +2.6 units, RIGHT
Corners:        square, 0 radius
Clear space:    1 bar height on all sides
Bar colors:     accent, ink, #6b6b63, #3B4031 (top to bottom)
```

HTML version (scale the px values together):

```html
<div style="display:flex;flex-direction:column;gap:7px">
  <div style="width:96px;height:21px;background:#C6F24A;margin-left:18px"></div>
  <div style="width:96px;height:21px;background:#F2F1EC"></div>
  <div style="width:96px;height:21px;background:#6b6b63"></div>
  <div style="width:96px;height:21px;background:#3B4031"></div>
</div>
```

Reductions: 3 bars at 16–31px. Filled square tile for avatars/favicons.
`≡≡` as the ASCII stand-in in CLI headers.

On light grounds drop the accent entirely — the stack descends in value
instead: `#14140f`, `#57564f`, `#8E9285`, `#c9c6bc`.

NEVER: round the corners; add perspective, bevel or wood texture; make more
than one bar accent; offset the top bar left or align it flush; set the
wordmark in anything but the brand sans (Manrope Bold in live UI, Space
Grotesk Bold in the existing lockup files) at -0.05em.

---

## 6. Voice

Audience: a solo builder who has been burned by a framework before. They
don't want enthusiasm; they want to know what this decided for them, and why.

1. **Say the decision out loud.** "Postgres only. No ORM." Opinions are the
   product. Hedging reads as a framework trying to be everything.
2. **Name the cost in the same breath.** Every claim gets its limitation.
   This is what buys trust from skeptics, and it costs nothing.
3. **Craft words, not platform words.** cut, fit, square, stock, own,
   seasoned. Not: leverage, unlock, seamless, supercharge.
4. **Second person, present tense.** "You own this file." Never
   "developers can now…" — that's a changelog written for investors.

Banned: seamless, effortless, blazing fast, magic, game-changing,
revolutionary, robust, leverage, 10x. No exclamation marks. No emoji
anywhere — not docs, not commits, not CLI output.

Write this / not this:

- "Your app keeps working if gorbital disappears."
  NOT "A powerful, future-proof foundation for modern Go apps."
- "Auth is a library, so security fixes reach you with go get."
  NOT "Enterprise-grade authentication out of the box!"
- "No MySQL, no SQLite. If that rules you out, it rules you out."
  NOT "Flexible database support coming soon."

---

## 7. CLI voice

The terminal is the brand's main surface. A developer reads a thousand lines
of this and maybe one page of the website.

Commands — eight ordinary verbs, no subcommand tree deeper than two:

```
orb new <name>            orb upgrade [module]
orb add <module>          orb dev
orb remove <module>       orb doctor
orb gen resource <Name>   orb search <term>
```

`new` not init. `add` not install. `doctor` not validate.
If a command needs a flag to be safe, it's the wrong command.

Output grammar:

```
+       file created                     (accent)
~       file edited — always names the anchor
✓       done, past tense
!       conflict or refusal — never a crash   (danger)
next:   every command ends by naming the next one
[y/N]   confirmation defaults to no, always
```

Lowercase sentences, no full stops on status lines, relative paths only.
No spinners, no progress bars, no ASCII art banner. Color is decoration —
strip it with `NO_COLOR` and the output still reads.

Errors are three parts, no blame: what happened (one line, with the file) /
what was done anyway (partial work is always reported) / the exact text or
command that fixes it. Never "see the docs".

Stable exit codes and `--json` on everything. The CLI refuses to run on a
dirty tree so `git checkout .` is always the undo button — say that in the
refusal message every time.

---

## 8. Module trust badges

Trust is typographic, never colorful. Traffic-light colors are wrong here:
unverified is not an error.

```
OFFICIAL   accent fill  #C6F24A, text #0B0C0A   — only official gets the fill
VERIFIED   1px border #F2F1EC, text #F2F1EC
LISTED     1px border #4a4a42, text #A8AB9F
```

11px mono, 0.08em tracking, 3px 7px padding, square.

---

## 9. Copy-paste CSS custom properties

```css
:root {
  --ground:   #0B0C0A;
  --surface:  #16180F;
  --code-bg:  #090A08;
  --accent:   #C6F24A;
  --ink:      #F2F1EC;
  --muted:    #A8AB9F;
  --dim:      #8E9285;
  --hairline: #22251C;
  --border:   #2B2F23;
  --border-2: #3B4031;
  --danger:   #FF5C2B;

  --paper:      #fbfaf6;
  --ink-dark:   #14140f;
  --secondary:  #57564f;

  --font-sans: "Manrope", ui-sans-serif, system-ui, sans-serif;
  --font-mono: "Geist Mono", ui-monospace, monospace;

  --r-sm: 8px;
  --r: 12px;
  --r-lg: 16px;
  --r-pill: 999px;
}
```

---

## 10. Still to confirm before first commit

Four lookups, none of which I can do for you:

1. `gorbital.dev` domain
2. the `gorbital` GitHub org
3. pkg.go.dev, for a module-path collision
4. a trademark search in your jurisdiction — "stock" has financial-sector
   marks around it, so check the class you'd file in

The module path is the one decision that cannot be changed later without a v2.
