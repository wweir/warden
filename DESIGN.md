# Design System

## Theme

Light default. Dark mode not currently implemented. The surface is intentionally light-gray (not white) to reduce eye strain during long debugging sessions.

## Color Strategy

Restrained: tinted neutrals + single blue accent (~8% of surface).

| Token | Value | Role |
|-------|-------|------|
| --c-primary | #4f6ef7 | Accent: links, active states, buttons |
| --c-primary-bg | #eef1fe | Subtle accent backgrounds |
| --c-bg | #f1f5f9 | Page background |
| --c-surface | #ffffff | Cards, panels, inputs |
| --c-text | #1e293b | Primary text |
| --c-text-2 | #475569 | Secondary text |
| --c-text-3 | #64748b | Muted text, timestamps |
| --c-border | #e2e8f0 | Card borders, dividers |
| --c-border-light | #f1f5f9 | Subtle separators |
| --c-success | #10b981 | OK status |
| --c-success-bg | #d1fae5 | OK pill background |
| --c-warning | #f59e0b | Warn/recovered status |
| --c-warning-bg | #fef3c7 | Warn pill background |
| --c-danger | #ef4444 | Error/rejected status |
| --c-danger-bg | #fee2e2 | Error pill background |

## Typography

System font stack. No custom fonts.
- Body: 13-14px, line-height 1.5
- Eyebrows/labels: 10-11px, uppercase, letter-spacing 0.08em, weight 700
- Monospace: `ui-monospace, SFMono-Regular, Menlo, monospace` for JSON, IDs, code

## Spacing

- Page padding: 24px
- Panel gap: 12-20px
- Card padding: 12-16px
- Border radius: 6px (--radius), 4px (--radius-sm)

## Components

- **Buttons**: `btn-primary` (solid accent), `btn-secondary` (bordered, transparent bg)
- **Pills**: rounded-full badges for status indicators
- **Panels**: white surface, 1px border, 6px radius — no shadow
- **Tables**: sticky header, `table-layout: fixed`, compact rows
- **Inputs**: bordered, focus ring via `box-shadow: 0 0 0 3px var(--c-primary-bg)`
- **Details/Summary**: native HTML disclosure widgets for progressive disclosure
