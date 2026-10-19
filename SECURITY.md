# Security policy

PlexRouter holds other people's API keys and money, so we take security reports seriously.

## Reporting a vulnerability

**Please don't open a public issue for security problems.** Report them privately through GitHub: go to the repository's **Security** tab and click **Report a vulnerability** ([direct link](https://github.com/local4a/plexrouter/security/advisories/new)).

Please include:

- what an attacker can do, and what they need (an account? an API key? admin?)
- steps or a request to reproduce it
- the PlexRouter version (`plexrouter -version`) and how it's deployed

We aim to acknowledge reports within 3 working days and to ship a fix or mitigation as soon as possible, depending on severity. We'll credit you in the release notes unless you prefer not to be named.

## Supported versions

Security fixes go into the latest release. Before 1.0, please upgrade to the newest version to receive them.

## Scope

In scope: the PlexRouter server and web console in this repository, for example authentication and session handling, API-key handling, billing (charging, credits, limits), access control between users and admins, and leaks of provider keys or other users' data.

Out of scope: vulnerabilities in upstream model providers, in a deployment's own configuration (for example running without HTTPS or exposing the data volume), and denial of service by sending very large volumes of traffic.

## Hardening tips for operators

- Serve PlexRouter over HTTPS (see "Production deployment" in the README) and set `PLEXROUTER_TRUST_PROXY=true` only behind a proxy you control.
- Set `PLEXROUTER_SECRET` yourself and back it up; it encrypts provider keys at rest.
- Set `PLEXROUTER_ADMIN_EMAILS`, so admin rights don't go to whoever signs in first.
- Back up the data volume, and keep it private: it contains the database and, unless set, the generated secret.
