# Known differences from GitLab

Differences that are intended. Any other difference found against a real server is a bug in GitLabAnak (ADR-0003).

- **New projects start with a commit.** GitLabAnak creates the default branch with one empty-tree commit so the branch exists. A new empty project on real GitLab has no branch until the first file is added (ADR-0002). Run this suite against a real project that already has a default branch.
- **Unbuilt endpoints answer 501.** Anything outside the call table returns `501 {"message": "not implemented"}` and is listed in `/_admin/requests`. A real server would answer something else.
- **Branch deletion is not implemented**, so the suite's cleanup of branches only works on a real server. GitLabAnak is reset between runs instead.
