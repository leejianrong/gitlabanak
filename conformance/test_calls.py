"""One test per row of the call table in docs/PLAN.md, driven through python-gitlab."""
import pytest
from gitlab import exceptions


def test_open_the_project(project, pid):
    assert project.id == pid


def test_file_create_read_update_delete(project, default_branch, uid):
    path = f"conformance/{uid}.yaml"
    project.files.create({"file_path": path, "branch": default_branch, "content": "a: 1\n", "commit_message": "create"})

    f = project.files.get(file_path=path, ref=default_branch)
    assert f.decode() == b"a: 1\n"
    assert f.encoding == "base64"
    for field in ("file_name", "file_path", "size", "content_sha256", "ref", "blob_id", "commit_id", "last_commit_id"):
        assert getattr(f, field), field

    f.content = "a: 2\n"
    f.save(branch=default_branch, commit_message="update")
    assert project.files.get(file_path=path, ref=default_branch).decode() == b"a: 2\n"

    f.delete(branch=default_branch, commit_message="delete")
    with pytest.raises(exceptions.GitlabGetError) as err:
        project.files.get(file_path=path, ref=default_branch)
    assert err.value.response_code == 404


def test_branch_create_get_and_isolation(project, default_branch, uid, cleanup):
    name = f"branch-{uid}"
    path = f"conformance/{uid}.txt"
    project.files.create({"file_path": path, "branch": default_branch, "content": "base\n", "commit_message": "base"})
    cleanup.append(lambda p: p.files.delete(path, default_branch, "cleanup"))

    branch = project.branches.create({"branch": name, "ref": default_branch})
    cleanup.append(lambda p: p.branches.delete(name))
    assert branch.name == name
    assert project.branches.get(name).name == name

    # a new branch starts as a copy, and a commit on it does not reach the default branch
    assert project.files.get(file_path=path, ref=name).decode() == b"base\n"
    project.files.create({"file_path": f"conformance/{uid}-only.txt", "branch": name, "content": "x", "commit_message": "on branch"})
    with pytest.raises(exceptions.GitlabGetError):
        project.files.get(file_path=f"conformance/{uid}-only.txt", ref=default_branch)

    with pytest.raises(exceptions.GitlabCreateError) as err:
        project.branches.create({"branch": name, "ref": default_branch})
    assert "already exists" in err.value.error_message


def test_commits_tree_and_raw(project, default_branch, uid, cleanup):
    path = f"conformance/{uid}-read.txt"
    project.files.create({"file_path": path, "branch": default_branch, "content": "raw bytes\n", "commit_message": f"read {uid}"})
    cleanup.append(lambda p: p.files.delete(path, default_branch, "cleanup"))

    newest = project.commits.list(ref_name=default_branch, per_page=1, get_all=False)[0]
    assert newest.title == f"read {uid}"
    assert len(newest.id) == 40 and newest.short_id and newest.parent_ids
    assert project.commits.get(newest.id).id == newest.id
    with pytest.raises(exceptions.GitlabGetError) as err:
        project.commits.get("0" * 40)
    assert err.value.response_code == 404

    names = [e["path"] for e in project.repository_tree(path="conformance", ref=default_branch, get_all=True)]
    assert path in names
    assert project.files.raw(file_path=path, ref=default_branch) == b"raw bytes\n"
