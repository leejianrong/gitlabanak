# Contributing

Run `make ci` before you push; CI runs the same checks. `make` lists the other targets.

## Do not use GitLab source code

GitLabAnak is written from the public GitLab documentation, from `python-gitlab`, and from responses observed on a running server (ADR-0004). Do not read GitLab's source (Community or Enterprise Edition) while working on this project, and do not paste any of it into an issue, a pull request or a comment. If you know behaviour only from reading that source, describe what you saw a server do instead.

## Conventions

- One change per pull request, with tests. Put the net line change in the description.
- Behaviour that clients can see belongs in the conformance suite (`conformance/`). A new error text starts as `"unverified": true` until someone has checked it against a real server.
- Only `internal/gitstore` imports go-git (ADR-0002). A new third-party dependency needs an ADR.
- Never log or commit tokens.
