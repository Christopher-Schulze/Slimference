# T316: TUI Status Polling Responsiveness

## Why

The TUI still felt laggy after removing cursor-move persistence because the
BubbleTea update loop refreshed expensive product, route, Desktop, and service
state too often. Those synchronous checks can block the update loop, so
keypresses wait behind status polling.

## Acceptance

- Pure cursor movement does not trigger product, route, Desktop, service, or
  persistence writes.
- Normal ticks run at a calmer cadence.
- Product status is cached and refreshed at most every few seconds unless
  forced.
- Route/Desktop/transparent service status is cached substantially longer and
  refreshed immediately only on explicit setup/status actions.
- Tests prove fresh product status is not re-fetched on every tick.
- Full CI passes.
- Latest binary is rebuilt, installed, and daemon-restarted.

## Sub-Tasks

- [x] Raise normal TUI tick interval from 500ms to 1s.
- [x] Add product-status cache timestamp and 5s refresh floor.
- [x] Raise service/route/Desktop status refresh floor from 2s to 30s.
- [x] Keep forced refreshes on explicit status/setup/launch paths.
- [x] Add regression test for fresh product-status cache reuse.
- [x] Run targeted TUI/launch tests and full CI.
- [x] Build and install the latest binary.

## Notes

- This preserves correctness for daily use because the expensive state is not
  request-critical. Explicit actions still force fresh reads.
- The product route signal can lag by a few seconds in the menu, but keyboard
  responsiveness wins. Real traffic still appears through request/flight events.
