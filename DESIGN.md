# TelcoPulse visual system

Operators watch a wall of telemetry in a dim ITOC during a long incident shift; a low-glare navy console keeps attention on customer impact and changing signals.

- Restrained palette: navy/charcoal surfaces, off-white type, crimson primary actions. User-provided references and palette override the skill's random color seed.
- Semantic green, amber, red, blue, and neutral status colors always accompany text and icons.
- OKLCH tokens live in apps/web/src/app/globals.css. Compact system sans with monospaced correlation identifiers and tabular numbers.
- Fixed header, narrow sidebar, dense tables, bordered evidence panels. Corners 6–8px. No decorative glow or gradients.
- Dashboard grids collapse structurally. Mobile navigation uses an accessible dialog. Tables retain horizontal scrolling.
- shadcn primitives provide inputs, buttons, dialogs, badges, skeletons, alerts and empty states.
- Metrics must represent measured observations. Unknown telemetry and unconfigured integrations must remain explicit.
