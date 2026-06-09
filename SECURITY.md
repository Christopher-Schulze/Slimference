# Security Policy

## Supported Versions

Slimference currently supports the latest code on `main` until the first public
release line is cut. Security fixes are applied there first.

## Reporting a Vulnerability

Do not include secrets, private prompts, captured traffic, or private repository
content in a public issue.

Use GitHub private vulnerability reporting if it is enabled for this repository.
If it is not enabled, open a minimal public issue that says a vulnerability needs
private coordination, without exploit details or sensitive data.

## Data Boundary

Slimference is designed for local, fail-open Codex routing. Normal product flows
must not require global system proxy settings, trusted local CA installation, or
machine-wide traffic interception.
