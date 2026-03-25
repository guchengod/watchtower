# Changelog

All notable changes to this project will be documented in this file.

## [1.1.0] - 2026-03-25

### Added

- HTTP API server built on Gin framework with the following endpoints:
  - `GET /api/v1/containers` - List all containers
  - `POST /api/v1/containers/:id/pause` - Pause a container
  - `POST /api/v1/containers/:id/resume` - Resume a container
  - `GET /api/v1/schedule` - Get current cron schedule
  - `PUT /api/v1/schedule` - Update cron schedule at runtime
- WebSocket hub for real-time dashboard updates (`pkg/api/websocket`)
- Runtime-updatable cron scheduler (`pkg/api/scheduler`)
- Recover middleware for panic recovery in HTTP API
- CORS middleware for dashboard access
- Configurable HTTP API port via `--http-api-port` / `WATCHTOWER_HTTP_PORT`

### Changed

- Upgraded Docker SDK to v28 and dependent packages
- HTTP API server now powered by Gin framework (from custom net/http)

### Fixed

- Documentation now includes all HTTP API-related flags
