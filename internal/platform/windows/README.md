# Windows platform adapter

Windows-only implementations belong here: Service Control Manager, named
pipes, ACLs, desktop/session APIs, process control, input, and power actions.
They must implement `internal/platform` ports and never be imported by Domain
or Application packages.
