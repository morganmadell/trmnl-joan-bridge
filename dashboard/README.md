# dashboard/lovelace-joan.yaml

## 1. What this file is

`lovelace-joan.yaml` is a real, working Home Assistant Lovelace dashboard
configuration. It was exported directly from a live, running Home Assistant
instance (not hand-written, not a generic template) and is the actual
dashboard used to drive this project's Joan-6 e-ink panel bridge.

It has exactly the 3 pages (Lovelace "views") the bridge expects:

- `sensors` — plant/environment sensor readings, grouped by area
- `controls` — media player status for the TVs the bridge can control
- `graphs` — history graphs for the same sensors

The bridge renders each of these pages to the Joan-6 panel as an image and
maps physical touch coordinates on the panel to Lovelace UI elements using
`bridge/zones.json` (a separate touch-zone layout file — see section 6
below). The page paths (`sensors`, `controls`, `graphs`) in this dashboard
must match the `"path"` fields `zones.json` expects; if you rename or
reorder these views, `zones.json` must be updated to match.

**This is a worked example, not a generic starter template.** Every sensor,
media player, and area name in this file is specific to the one Home
Assistant installation it was exported from. It will not work as-is against
your own Home Assistant instance — see section 4 for exactly what you need
to change.

## 2. How to import it (recommended)

Home Assistant has a native "Raw configuration editor" for dashboards that
lets you paste a full dashboard config directly, without touching
`configuration.yaml` or restarting Home Assistant. This is the recommended
way to bring this file in:

1. In Home Assistant, go to **Settings → Dashboards**.
2. Click **+ Add Dashboard**, then choose **New dashboard from scratch**.
3. Give it a name and, importantly, set its **URL path** deliberately. This
   project's bridge requests dashboard pages by URL, e.g.
   `/lovelace-joan/sensors`. The dashboard's `url_path` is the segment
   right after the leading slash — concretely, `lovelace-joan` in that
   example — and it must match whatever `bridge/zones.json`'s `"path"`
   fields expect. When creating the dashboard, check the URL Home
   Assistant assigns it (or set it explicitly if the creation dialog
   allows it) and make sure it lines up with what `zones.json` is
   configured to use — e.g. `lovelace-joan`.
4. Open the new (currently empty) dashboard.
5. Click the **three-dot menu** in the top right corner and choose
   **Edit Dashboard**.
6. Click the **three-dot menu** again (now inside edit mode) and choose
   **Raw configuration editor**.
7. Select all of the placeholder YAML/JSON that's there and delete it.
8. Open `lovelace-joan.yaml` from this directory, copy its entire contents,
   and paste them into the raw configuration editor.
9. Click **Save**.

That's it — no `configuration.yaml` edit, no restart. This works because a
dashboard created this way is stored in Home Assistant's own internal
`.storage` directory (this is what Home Assistant calls "UI-managed" mode),
which is exactly how the original dashboard this file was exported from
exists today. The raw configuration editor is just a convenient way to
replace that stored config wholesale in one paste instead of rebuilding
every card by hand through the UI.

## 3. Alternative: YAML-mode dashboard (optional, not the default recommendation)

Home Assistant also supports a second way to define dashboards: file-based
YAML-mode dashboards, configured directly in `configuration.yaml`:

```yaml
lovelace:
  dashboards:
    lovelace-joan:
      mode: yaml
      filename: dashboard/lovelace-joan.yaml
      title: Joan Display
```

After adding a block like this, Home Assistant needs a **full restart**
(not just a config reload) to pick it up.

The tradeoff: once a dashboard is defined this way, it is no longer
editable from the Home Assistant UI — the file becomes the single source of
truth, and any UI edits are effectively disabled (or silently discarded,
depending on HA version). This is useful if you specifically want your
dashboard version-controlled and auto-loaded on every Home Assistant
startup. But for most people, and as the default recommendation in this
project, **use the raw configuration editor approach in section 2 instead**
— it is simpler, doesn't require a restart, and keeps the dashboard
UI-editable going forward, matching how the original dashboard this file
was exported from is actually managed.

## 4. What MUST be customized per installation

This dashboard will not show meaningful data until you replace the
following with entities and values from your own Home Assistant instance:

- **`sensor.plant_sensor_*` entities** (Sensors page) — temperature,
  illuminance, moisture, and conductivity sensors, across 3 areas in the
  source deployment: `north_side`, `boardroom`, and `dana`. Replace each of
  the 12 `sensor.plant_sensor_<area>_<measurement>` entity IDs with your own
  sensor entities. If you don't have an equivalent sensor for a given area,
  remove that area's entire card block (the area heading plus its 4-card
  grid) rather than leaving broken entity references in place.

- **`media_player.*` entities** (Controls page) — there are 4 of them:
  `media_player.boardroom_tv`,
  `media_player.boardroom_tv_un85du6900fxzc`,
  `media_player.chromecast3441`, and
  `media_player.tv_samsung_8_series_82`.
  Replace each with your own `media_player` entity IDs (find yours under
  **Settings → Devices & Services → Entities**, filtered to the
  `media_player` domain).

- **`sensor.bermuda_global_*` entities** (Sensors page, "BLE Presence"
  section) — these come from the [Bermuda BLE
  Trilateration](https://github.com/agittins/bermuda) Home Assistant
  integration and report Bluetooth presence-detection counts. If you don't
  use Bermuda, delete this section (the "## BLE Presence (Global)" heading
  card and the 4-card grid beneath it) rather than leaving it pointing at
  nonexistent entities.

- **Area/section heading text** — the plain Markdown headings "Break Area",
  "Big Boardroom", "Small Boardroom — no sensor installed", and
  "R&D Cubicles (Dana)" are cosmetic labels describing the original
  deployment's physical spaces. They don't need to match any entity or
  Home Assistant object — just edit the text to describe your own spaces.

- **`history-graph` entities** (Graphs page) — the two `history-graph` cards
  reference the same `sensor.plant_sensor_*` and `sensor.bermuda_global_*`
  entities used on the Sensors page, with friendly `name:` labels. Update
  these to match whatever entities you kept after the changes above.

## 5. Why every card is a `markdown` card with heading levels

Every single card in this dashboard is a `type: markdown` card, and every
one of them uses Markdown heading syntax (`#`, `##`) combined with `<small>`
and `<br>` HTML tags for layout and text sizing — for example:

```
# <small>Temp</small><br>{{ states('sensor.plant_sensor_north_side_temperature') }}°C
```

This isn't a stylistic preference — it's a direct consequence of a
sanitization constraint in Home Assistant's `markdown` card. The card
renders through a Markdown-to-HTML sanitizer that strips out inline
`style` attributes and raw CSS layout constructs — things like
`<div style="font-size: 2em">` or `display: grid` — for security reasons.
Only genuine Markdown syntax (heading levels `#` through `######`, `<br>`,
`<small>`, bold/italic, etc.) and native Lovelace card types actually
survive and render.

That's why this dashboard leans so heavily on heading levels for text
sizing: a top-level `#` heading renders large (used for the actual sensor
value), while `<small>` wrapped text renders small (used for the label
above it) — and `<br>` joins the label and value into a single Markdown
block to minimize wasted vertical whitespace between them, since two
separate cards would each carry their own card padding/margin. Every
"label + big value" card in this file follows that same
`# <small>Label</small><br>{{ value }}` pattern for exactly this reason.

If you want more real layout or styling control than native Markdown
headings give you — actual font sizes, colors, custom spacing — the
[card-mod](https://github.com/thomasloven/lovelace-card-mod) HACS
component removes this constraint entirely, letting you apply real CSS to
any card. It is **not installed** in this project by default; it's a
third-party component you'd install yourself via HACS if you want it. The
tradeoff is that `card-mod` works by patching into Home Assistant's
frontend shadow DOM, which means it can break across Home Assistant version
upgrades — something to weigh before taking on that dependency.

## 6. Keeping `bridge/zones.json` in sync

`bridge/zones.json` defines the physical touch-zone rectangles (pixel
coordinates on the Joan-6 e-ink panel) that the bridge maps to elements of
this dashboard, per page. It is tied to the exact visual layout this
dashboard produces — the number of areas, the number of rows in the
Controls page, the text/card sizes, etc.

If you customize this dashboard in ways that change its rendered layout —
adding or removing an area's card block, changing how many Controls rows
there are, changing text/card sizes, reordering views — the touch-zone
rectangles recorded in `zones.json` will no longer line up with what's
actually on screen and must be re-measured. See `bridge/README.md`'s
"Re-measuring zones after a layout change" (under "Troubleshooting and
touch calibration") for how — this file just calls it out as a step you
need after changing this dashboard, rather than duplicating it here.
