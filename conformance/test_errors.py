"""One test per row of the error table in docs/PLAN.md."""
import pytest
import requests

from conftest import check


def create_file(api, path, branch, content="x\n"):
    return api.call("POST", api.file_path(path), json={"branch": branch, "content": content, "commit_message": "conformance"})


def test_unauthorized(base, pid):
    url = f"{base}/api/v4/projects/{pid}"
    check("unauthorized", requests.get(url, timeout=30))
    check("unauthorized", requests.get(url, headers={"PRIVATE-TOKEN": "wrong"}, timeout=30))


def test_project_not_found(api):
    r = requests.get(f"{api.base}/api/v4/projects/999999999", headers={"PRIVATE-TOKEN": api.token}, timeout=30)
    check("project_not_found", r)


def test_file_not_found(api, default_branch, uid):
    check("file_not_found", api.call("GET", api.file_path(f"no/such-{uid}.txt"), params={"ref": default_branch}))


def test_ref_missing(api, uid):
    check("ref_missing", api.call("GET", api.file_path(f"no/such-{uid}.txt")))


def test_branch_not_found(api, uid):
    check("branch_not_found", api.call("GET", f"/repository/branches/nope-{uid}"))


def test_branch_exists(api, default_branch):
    check("branch_exists", api.call("POST", "/repository/branches", json={"branch": default_branch, "ref": default_branch}))


def test_invalid_ref(api, uid):
    ref = f"nope-{uid}"
    check("invalid_ref", api.call("POST", "/repository/branches", json={"branch": f"new-{uid}", "ref": ref}), ref=ref)


def test_file_exists(api, default_branch, uid, cleanup):
    path = f"conformance/{uid}.txt"
    assert create_file(api, path, default_branch).status_code == 201
    cleanup.append(lambda p: p.files.delete(path, default_branch, "cleanup"))
    check("file_exists", create_file(api, path, default_branch))


def test_update_and_delete_of_a_missing_file(api, default_branch, uid):
    path = api.file_path(f"conformance/missing-{uid}.txt")
    body = {"branch": default_branch, "content": "x", "commit_message": "conformance"}
    check("file_missing", api.call("PUT", path, json=body))
    check("file_missing", api.call("DELETE", path, params={"branch": default_branch, "commit_message": "conformance"}))


def test_file_operation_on_a_missing_branch(api, uid):
    check("not_on_branch", create_file(api, f"conformance/{uid}.txt", f"nope-{uid}"))


def test_unexpected_error_is_a_500_with_a_message():
    pytest.skip("cannot be provoked through the public API; the Go panic-recovery test covers it")
