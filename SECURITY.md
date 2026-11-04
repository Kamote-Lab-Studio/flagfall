# Security policy

Flagfall runs competitions where players are actively trying to break things, so we take reports about the **platform itself** seriously.

## In scope

Bugs in Flagfall's own code: authentication, sessions, the admin panel, flag storage, scoring, the Theme SDK, file serving, theme sandboxing, challenge container isolation.

## Out of scope

Intentionally vulnerable challenges in an event, or a specific event's configuration. Report those to that event's organizers.

## Reporting

Please **do not** open a public issue. Report privately through GitHub's [private vulnerability reporting](https://github.com/Kamote-Lab-Studio/flagfall/security/advisories/new).

If you can't use that, email eduardodbarrete@gmail.com with "Flagfall security" in the subject.

Include the version, steps to reproduce, and the impact you see. We'll acknowledge within 3 working days.

## Disclosure

We follow a 90-day coordinated disclosure policy: we aim to fix and release within 90 days of the report, and we'll agree a disclosure date with you. Reporters are credited in the release notes unless they ask not to be.

## Supported versions

Before 1.0, only the latest release gets security fixes. After 1.0, the latest minor release and the one before it.
