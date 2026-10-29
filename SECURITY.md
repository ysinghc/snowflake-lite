# Security Policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub: <https://github.com/ysinghc/snowflake-lite/security/advisories/new>

Include the affected version, a description, and steps to reproduce. Please do not open a public issue or discuss details publicly until a fix is released.

## Supported versions

Only the latest release receives fixes.

## Scope

The `snowflake` package, the `saas` package and `serve-saas`, and the project website are in scope. Several weaknesses of the `saas` reference implementation are already documented at <https://snowflake.ysinghc.in/docs/security-model/>.

Snowflake IDs are predictable by design; guessing the next ID is not a vulnerability.
