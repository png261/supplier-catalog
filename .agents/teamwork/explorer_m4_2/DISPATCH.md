# Explorer M4.2 Dispatch: Embedded UI & Frontend Dashboard Design

## Identity & Role
- Archetype: teamwork_preview_explorer
- Role: Embedded UI & Frontend Dashboard Explorer
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_2
- Parent Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8

## Inputs & Specifications
- Original Request: /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- Project Specification: /Users/png/01. Project/coderpush/PROJECT.md
- Existing Codebases: `pkg/domain`, `pkg/storage`, `pkg/sync`, `pkg/generator`

## Exploration Objectives
1. Design embedded static asset mechanism:
   - `web/embed.go` using standard Go 1.16+ `embed.FS` (`//go:embed static/*`).
   - Serves static assets at `/` and `/static/` with fallback to `index.html`.
2. Design single-page dashboard architecture (`web/static/`):
   - Zero node/npm dependencies: pure vanilla HTML5, CSS3, and ES6+ JavaScript.
   - Professional, responsive dark/light engineering UI:
     - Header: System title, active DB status, active job badge (ID, status, runtime).
     - Control Panel: Start Sync Job form (configure generator volume, seed, anomaly ratios, or run default), Resume Interrupted Job button with dropdown of interrupted jobs.
     - Metric Cards (5 live indicators):
       - `+ Added` (Green)
       - `~ Updated` (Blue)
       - `- Withdrawn` (Amber)
       - `↷ Skipped` (Gray / Purple)
       - `⚠ Dead-Lettered` (Red)
     - Real-Time Progress Bar: Batch completion percentage, ETA / speed, and live event log.
     - SSE Listener: Automatic reconnection (`EventSource`), status indicator (Connected/Disconnected/Reconnecting).
     - Dead-Letter Queue Inspector Table: Paginated view showing Job ID, Batch Index, Record Index, Defect Reason, and clickable "View Raw Payload" modal.
     - Catalog Products Explorer: Searchable, paginated table of current catalog state.
3. Document full UI wireframes, DOM structure, CSS design tokens, and JavaScript event loops in `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_2/handoff.md`.


## 2026-10-21T13:04:35Z
You are teamwork_preview_explorer. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_2. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_2/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Design embedded static asset mechanism (web/embed.go) and single-page dashboard UI (HTML/CSS/JS with zero node/npm dependencies, live counters, control plane, dead-letter inspector). Write report to /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_2/handoff.md. Send message when done.
