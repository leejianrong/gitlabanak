"""What an application's integration test looks like against GitLabAnak.

The scenario: a manifest file that is registered on a branch, read back on the branch and on
main, updated and deleted. Run with `make consumer IMAGE=ghcr.io/leejianrong/gitlabanak:0.1.0`.
"""
import os

import gitlab
import pytest
from gitlab.exceptions import GitlabCreateError, GitlabGetError


@pytest.fixture
def project():
    gl = gitlab.Gitlab(os.environ["GITLAB_URL"], private_token=os.environ["GITLAB_TOKEN"])
    return gl.projects.get(int(os.environ["GITLAB_PROJECT_ID"]))


def test_manifest_on_branch_and_main(project):
    manifest = "manifests/app.yaml"
    project.branches.create({"branch": "ns1", "ref": "main"})

    # On the branch only.
    project.files.create({"file_path": manifest, "branch": "ns1", "content": "v: 1\n", "commit_message": "register"})
    assert project.files.get(manifest, ref="ns1").decode() == b"v: 1\n"
    with pytest.raises(GitlabGetError) as missing:
        project.files.get(manifest, ref="main")
    assert missing.value.response_code == 404

    # Update, then register the same path on main too.
    f = project.files.get(manifest, ref="ns1")
    f.content = "v: 2\n"
    f.save(branch="ns1", commit_message="update")
    project.files.create({"file_path": manifest, "branch": "main", "content": "v: 9\n", "commit_message": "on main"})
    assert project.files.get(manifest, ref="ns1").decode() == b"v: 2\n"
    assert project.files.get(manifest, ref="main").decode() == b"v: 9\n"

    # A second create is refused, and delete removes it from the branch only.
    with pytest.raises(GitlabCreateError) as dup:
        project.files.create({"file_path": manifest, "branch": "ns1", "content": "x", "commit_message": "again"})
    assert dup.value.response_code == 400
    project.files.delete(manifest, branch="ns1", commit_message="remove")
    with pytest.raises(GitlabGetError):
        project.files.get(manifest, ref="ns1")
    assert project.files.get(manifest, ref="main").decode() == b"v: 9\n"
