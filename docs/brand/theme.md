# apistock — theme reference

Paste this into Claude Code as project context. Everything below is the
canonical visual system for apistock: docs, landing page, README, CLI output.

---

## 1. Identity

Name:      apistock — ALWAYS lowercase, one word.
           Never APIStock, ApiStock, api-stock, APIstock.
           Lowercase even at the start of a sentence (rewrite the sentence
           instead of capitalising).
Spoken:    "A-P-I stock"
Binary:    `aps`
Module:    github.com/apistock/apistock

Tagline:       The app a careful senior Go engineer would have set up.
Short form:    Prepared stock for Go APIs.
Internal only: "Thin glue, thick library." (explains architecture to
               engineers; never put it on a landing page)

Meaning: "stock" is the prepared material a maker starts from — milled
square, seasoned, ready to cut. Not a finished thing, not a blind kit of
parts. Material of known quality that you take to your own bench.

---

## 2. Color

Dark theme is primary. Light theme is for README, docs and print only.

### Dark (primary)

| Token    | Hex       | Use                                                  |
|----------|-----------|------------------------------------------------------|
| ground   | `#0c0c0a` | Page background. Warm black, not blue.               |
| surface  | `#131310` | Cards, panels. One step up from ground, never two.   |
| code-bg  | `#0a0a08` | Terminal blocks and code blocks. One step DOWN.      |
| accent   | `#d8ff3e` | The board being taken. One use per screen.           |
| ink      | `#f0efe9` | Headlines and primary body. 17:1 on ground.          |
| muted    | `#a5a59a` | Secondary prose. The floor for body text.            |
| dim      | `#8a8a80` | Labels, captions, inline comments in code.           |
| hairline | `#22221e` | 1px dividers. The ONLY divider. No shadows anywhere. |
| border   | `#2a2a24` | Card and panel borders.                              |
| border-2 | `#3a3a34` | Secondary buttons, chips.                            |
| danger   | `#ff8f6b` | Errors, conflicts, DON'T callouts. Never pure red.   |

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
- Text on a lime fill is always `#0c0c0a`.
- ONE accent moment per view. If two things are lime, neither is important.
- Never tint the wordmark accent.
- Max 1–2 background colors per page.

---

## 3. Type

Two families, from Google Fonts:

```html
<link href="https://fonts.googleapis.com/css2?family=Space+Grotesk:wght@400;500;700&family=JetBrains+Mono:wght@400;500;700&display=swap" rel="stylesheet">
```

- **Space Grotesk** (400/500/700) — headlines, UI, prose.
  Technical, slightly mechanical grotesque. Deliberately NOT Inter.
- **JetBrains Mono** (400/500) — code, labels, data, terminal, all paths,
  payloads, headers, version numbers. Anything a developer would copy.

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
- Sections separated by `1px solid #22221e` + 48–72px padding.
- Square corners everywhere. `border-radius: 0`. No exceptions.
- NO shadows. Depth comes from the 1px hairline and the surface step only.
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
Bar colors:     accent, ink, #6b6b63, #3a3a34 (top to bottom)
```

HTML version (scale the px values together):

```html
<div style="display:flex;flex-direction:column;gap:7px">
  <div style="width:96px;height:21px;background:#d8ff3e;margin-left:18px"></div>
  <div style="width:96px;height:21px;background:#f0efe9"></div>
  <div style="width:96px;height:21px;background:#6b6b63"></div>
  <div style="width:96px;height:21px;background:#3a3a34"></div>
</div>
```

Reductions: 3 bars at 16–31px. Filled square tile for avatars/favicons.
`≡≡` as the ASCII stand-in in CLI headers.

On light grounds drop the accent entirely — the stack descends in value
instead: `#14140f`, `#57564f`, `#8a8a80`, `#c9c6bc`.

NEVER: round the corners; add perspective, bevel or wood texture; make more
than one bar accent; offset the top bar left or align it flush; set the
wordmark in anything but Space Grotesk Bold at -0.05em.

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

- "Your app keeps working if apistock disappears."
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
aps new <name>            aps upgrade [module]
aps add <module>          aps dev
aps remove <module>       aps doctor
aps gen resource <Name>   aps search <term>
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
OFFICIAL   accent fill  #d8ff3e, text #0c0c0a   — only official gets the fill
VERIFIED   1px border #f0efe9, text #f0efe9
LISTED     1px border #4a4a42, text #a5a59a
```

11px mono, 0.08em tracking, 3px 7px padding, square.

---

## 9. Copy-paste CSS custom properties

```css
:root {
  --ground:   #0c0c0a;
  --surface:  #131310;
  --code-bg:  #0a0a08;
  --accent:   #d8ff3e;
  --ink:      #f0efe9;
  --muted:    #a5a59a;
  --dim:      #8a8a80;
  --hairline: #22221e;
  --border:   #2a2a24;
  --border-2: #3a3a34;
  --danger:   #ff8f6b;

  --paper:      #fbfaf6;
  --ink-dark:   #14140f;
  --secondary:  #57564f;

  --font-sans: "Space Grotesk", system-ui, sans-serif;
  --font-mono: "JetBrains Mono", ui-monospace, monospace;

  --radius: 0;
}
```

---

## 10. Still to confirm before first commit

Four lookups, none of which I can do for you:

1. `apistock.dev` domain
2. the `apistock` GitHub org
3. pkg.go.dev, for a module-path collision
4. a trademark search in your jurisdiction — "stock" has financial-sector
   marks around it, so check the class you'd file in

The module path is the one decision that cannot be changed later without a v2.
