# internal/install

## Responsibilities

`internal/install` owns managed service installation helpers:

- Copies the current binary to platform-managed locations.
- Writes supervisor definitions for systemd, launchd, or Windows Task Scheduler.
- Creates the minimal managed bootstrap config when no config exists.
- Preserves platform-specific install behavior behind one `InstallService` API.

## Boundary

- The package does not start the normal foreground gateway runtime.
- The package writes host-level service/config files only through explicit install flows.
