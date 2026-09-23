# Security boundary and maintenance

SSHDock is a single-owner, single-node runtime for trusted Compose applications. A deploy key can operate all apps. Compose can mount host paths, use privileged containers or the Docker socket; Docker-group access is effectively host administration. Warnings about these settings are diagnostic, not a sandbox. Do not give deploy access to untrusted tenants.

Production remote access uses the installed OpenSSH forced Git and operator commands. The systemd daemon processes deployments; it does not expose a deployment HTTP API or require a separate SSH port. Keep the operating system, OpenSSH, Docker and Caddy patched and restrict the host's administrative SSH keys. The embedded development SSH server is not the bootstrap-installed access path.

Application config is encrypted on disk. Its local key, database and backup archives must be protected together. Config reveal and container exec intentionally expose requested data to the trusted operator. Redaction covers known stored config values in ordinary output, not arbitrary transformed values, secrets embedded in application images, or secrets deliberately revealed by an owner. Registry credentials belong to the Docker user/credential helper; they are not application config. See [external-build credentials](EXTERNAL_BUILDS.md#credentials-and-artifact-retention).

Build SSHDock with Go **1.26.8 or newer**. The minimum patch toolchain and `golang.org/x/crypto` update address the standard-library and SSH advisories identified by the v1 audit. CI runs `make ci` and `make security`; release packaging requires the same workflow to pass first. `make security` runs pinned `govulncheck` against the current official Go vulnerability database and fails on reachable vulnerable symbols. Run it again before releasing: a passing scan is evidence at that time, not a permanent absence-of-vulnerabilities claim.

The upstream [Go release history](https://go.dev/doc/devel/release) documents supported toolchain fixes. The [Go vulnerability database](https://pkg.go.dev/vuln/) provides the source advisories; scanners may report library call paths whose actual exposure depends on the configured access path. Application images and host packages need their own patching and scans.

Keep private deployment evidence, keys, production domains, backup paths and raw logs out of public issues. Report reproducible non-sensitive reliability problems in the issue tracker; arrange a private channel with a maintainer before sharing a security report containing exploit details or credentials.
