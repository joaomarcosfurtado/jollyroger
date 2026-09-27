# Security Policy

jollyroger mounts an administrative surface inside other people's applications, so we treat
security reports as the highest priority.

## Reporting a vulnerability

**Do not open a public issue.** Use GitHub's private vulnerability reporting:
[Report a vulnerability](https://github.com/joaomarcosfurtado/jollyroger/security/advisories/new).

Please include the affected version or commit, the configuration (auth mode, database), a
reproduction, and the impact you expect.

What to expect:
- acknowledgement within 3 working days;
- an assessment and a planned fix date within 10 working days;
- a coordinated disclosure: we publish a GitHub Security Advisory with the fixed version and credit
  you unless you prefer otherwise.

## Supported versions

Before v1.0.0, only the latest minor release receives security fixes. After v1.0.0, the latest two
minor releases do.

## Scope

In scope: the library, the dashboard, the HTTP API, the CLI, the Docker image, and the published
protocol documents. Out of scope: vulnerabilities that require an already-compromised host
application or database, and findings in example code that is clearly marked as insecure for local
demos (`InsecureNoAuth`).
