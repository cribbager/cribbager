# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities **privately** — don't open a public issue.

Use GitHub's private vulnerability reporting: go to the repository's **Security** tab
and click **"Report a vulnerability."** That opens a private advisory visible only to
the maintainer.

This is a hobby project maintained by one person, so responses are best-effort — I
aim to acknowledge a report within about a week. Please include enough detail to
reproduce the issue: steps, the affected endpoint or page, and the impact.

## Scope

In scope: this codebase and the deployed app at <https://cribbager.org> — for example
authentication and session handling, the password-reset flow, rate limiting, or
anything that could expose another player's hidden cards or account data.

Out of scope: findings that require a compromised machine or account, volumetric
denial-of-service, and issues in third-party dependencies (report those upstream).

Thanks for helping keep Cribbager safe.
