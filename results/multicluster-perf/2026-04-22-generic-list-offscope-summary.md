# Generic List Off-Scope Same-Kind Churn Summary (2026-04-22)

Environment:
- macOS darwin/arm64
- Apple M1
- package: `github.com/elijahrou/surfsk8s/internal/app`

Raw results:
- paired bench: `results/multicluster-perf/2026-04-22-generic-list-offscope-paired.txt`

Important benchmark note:
- earlier tick-based measurements for this scenario were misleading because `Update(tickMsg)` allocates live Bubble Tea timers via `tickCmd()`.
- corrected benchmark measures the refresh path directly.

What changed:
- added generic resource delta journal in manager
- added app-side generic row index + off-scope delta application
- preserved visible generic printer cache on off-scope same-kind updates
- made generic change history bounded without per-update slice reallocation

Observed result:
- off-scope same-kind generic-list refresh now measures about `15.5-16.7 µs/op`
- simulated unknown/full-reload fallback bench currently lands in the same range in this fixture harness, so this particular benchmark does not expose a large before/after delta

Interpretation:
- off-scope same-kind churn is no longer the likely hot path
- next likely ROI is in-scope but off-screen generic list churn, where current code still rebuilds visible/sorted state conservatively
