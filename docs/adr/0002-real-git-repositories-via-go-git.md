# ADR-0002: Back every project with a real Git repository using go-git, with deterministic commits

- Status: Accepted
- Date: 2026-11-27
- Deciders: project owner, Claude

## Context

The first draft modelled branches as in-memory file maps. The project owner rejected that: the product has to behave like a Git host, because users will clone and push (platform teams iterating on CI), and because the long-term aim is a free, self-hostable GitLab-compatible forge to sit next to Forgejo and Gitea. A fake model would be thrown away at 0.2.0.

Tests also need reproducibility. Real git commit ids depend on author, committer, timestamp, parent and tree, so a naive implementation gives different ids on every run, which breaks assertions on `/_admin/state`.

## Decision

- Each project is one bare git repository managed by go-git v5. Branches are real refs, commits are real commit objects, files are blobs in real trees.
- Default storage is go-git's in-memory storage. If `GITLAB_DATA_DIR` is set, repositories live on disk at `<dir>/projects/<id>.git` using filesystem storage and are reopened at start.
- Every commit the server creates uses a fixed identity (`GitLabAnak <gitlabanak@localhost>`) and a deterministic time: the parent commit's time plus one second, with a fixed epoch (2020-01-01T00:00:00Z) for a root commit. Same operations in the same order give the same commit ids across runs.
- A new project starts with one root commit holding the empty tree, on the default branch. Real GitLab shows a brand-new project with no branch. We diverge so the spec's start state ("`main` exists, no files") is true; the conformance suite records this as a known difference.
- Reads and writes to one project go through one mutex. Requests to different projects do not block each other.
- `ref` on file reads accepts branch names, tag names and commit SHAs, as the real API does.
- Smart-HTTP clone and push (0.2.0) is served from the same repositories using go-git's server-side transport, so the REST API and `git` see one state.

## Alternatives considered

| Option | Why not |
|--------|---------|
| In-memory maps that look like branches | Owner rejected it. It would be rewritten for 0.2.0, and it never tests behaviour like real tree and commit semantics. |
| Shell out to `git` | Needs git in the image (ADR-0001), and process spawn per request is slow. |
| Own git implementation | Large effort with no product value over go-git. |
| libgit2 bindings | Needs CGo, which breaks the static binary. |

## Consequences

- Buys: `git clone` and `git push` work against the same state, commit ids are real and stable, branch creation is cheap (a ref), and the path to a forge does not need a storage rewrite.
- Costs: go-git is slower than C git and lacks some features (for example, parts of the protocol v2 and some merge features). Acceptable at test-fixture scale. Finding the last commit that touched a file means walking history, so `last_commit_id` is O(history). Fine for fixtures, to be cached if it ever matters.
- Confirmed in slice V1 (go-git v5.19.3): go-git creates commits on a bare in-memory repository with no worktree, using the plumbing API. A test compares our root tree id with the one `git write-tree` produced for the same files, so tree encoding and ordering match real git.
- Risk to confirm in slice V4: that go-git's server transport handles `git push` with a real `git` client. If it does not, V4 gets a spike and may need a small custom receive-pack.
