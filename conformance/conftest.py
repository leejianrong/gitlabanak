"""Conformance suite: the same tests run against GitLabAnak and a real GitLab (ADR-0003).

Target: GITLAB_URL, GITLAB_TOKEN, GITLAB_PROJECT_ID (default 100).
Expected error responses live in expected/*.json. A row marked "unverified" is a guess from the
documentation that has never been checked against a real server; the run prints how many there are.
With CONFORMANCE_ENFORCE_BASELINE=1 the run fails if that number is above unverified_baseline.txt.
"""
import json
import os
import pathlib
import urllib.parse
import uuid

import gitlab
import pytest
import requests

HERE = pathlib.Path(__file__).parent
EXPECTED = {}
for f in sorted((HERE / "expected").glob("*.json")):
    EXPECTED.update(json.loads(f.read_text()))
USED = set()


def check(case_id, response, **fill):
    """Assert a raw response equals the recorded expectation for case_id."""
    want = EXPECTED[case_id]
    USED.add(case_id)
    body = json.loads(json.dumps(want["body"]).replace("{ref}", fill.get("ref", "{ref}")))
    try:
        got = response.json()
    except ValueError:
        got = response.text
    assert (response.status_code, got) == (want["status"], body), f"{case_id}: got {response.status_code} {got}"


class Api:
    def __init__(self, base, token, pid):
        self.base, self.token, self.pid = base, token, pid

    def url(self, path):
        return f"{self.base}/api/v4/projects/{self.pid}{path}"

    def call(self, method, path, **kw):
        headers = {"PRIVATE-TOKEN": self.token, **kw.pop("headers", {})}
        return requests.request(method, self.url(path), headers=headers, timeout=30, **kw)

    def file_path(self, path):
        return "/repository/files/" + urllib.parse.quote(path, safe="")


@pytest.fixture(scope="session")
def base():
    return os.environ["GITLAB_URL"].rstrip("/")


@pytest.fixture(scope="session")
def token():
    return os.environ["GITLAB_TOKEN"]


@pytest.fixture(scope="session")
def pid():
    return int(os.environ.get("GITLAB_PROJECT_ID", "100"))


@pytest.fixture(scope="session")
def api(base, token, pid):
    return Api(base, token, pid)


@pytest.fixture(scope="session")
def project(base, token, pid):
    return gitlab.Gitlab(base, private_token=token).projects.get(pid)


@pytest.fixture(scope="session")
def default_branch(project):
    return project.default_branch


@pytest.fixture
def uid():
    return uuid.uuid4().hex[:8]


@pytest.fixture
def cleanup(project):
    """Best-effort removal of what a test created. GitLabAnak cannot delete branches yet, and
    does not need to: it is reset between runs."""
    actions = []
    yield actions
    for act in reversed(actions):
        try:
            act(project)
        except Exception:
            pass


def pytest_terminal_summary(terminalreporter):
    unverified = sorted(i for i in USED if EXPECTED[i].get("unverified"))
    terminalreporter.write_line(f"conformance: {len(USED)} error rows exercised, {len(unverified)} unverified: {', '.join(unverified)}")


def pytest_sessionfinish(session, exitstatus):
    if os.environ.get("CONFORMANCE_ENFORCE_BASELINE") != "1" or exitstatus != 0:
        return
    unverified = sum(1 for i in USED if EXPECTED[i].get("unverified"))
    baseline = int((HERE / "unverified_baseline.txt").read_text())
    if unverified > baseline:
        print(f"\nFAIL: {unverified} unverified rows exceed the baseline of {baseline}")
        session.exitstatus = 1
