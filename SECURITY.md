# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in ai-shim, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please email: **zaephor@users.noreply.github.com**

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

## Response Timeline

- **Acknowledgment:** within 48 hours
- **Initial assessment:** within 7 days
- **Fix or mitigation:** depends on severity, targeting 30 days for critical issues

## Scope

The following are in scope:
- Container escape or privilege escalation
- Shell injection via config values
- Path traversal in volume mounts or data directories
- Credential exposure (beyond Docker's inherent `docker inspect` visibility)
- Denial of service via malformed config

The following are out of scope:
- Issues requiring root access to the host
- Docker daemon vulnerabilities (report to Docker)
- Attacks requiring physical access
- Social engineering

## Supported Versions

Only the latest release is supported with security updates.
