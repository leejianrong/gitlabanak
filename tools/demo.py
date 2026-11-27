"""Exercise the file API with python-gitlab, the most common client.
Usage: demo.py URL TOKEN"""
import sys

import gitlab

gl = gitlab.Gitlab(sys.argv[1], private_token=sys.argv[2])
project = gl.projects.get(100)
path = "pipelines/ns1/manifest.yaml"

project.files.create({"file_path": path, "branch": "main", "content": "a: 1\n", "commit_message": "create"})
f = project.files.get(file_path=path, ref="main")
print("created:", f.decode().decode(), "commit", f.commit_id)

f.content = "a: 2\n"
f.save(branch="main", commit_message="update")
f = project.files.get(file_path=path, ref="main")
print("updated:", f.decode().decode(), "commit", f.commit_id, "last", f.last_commit_id)

f.delete(branch="main", commit_message="delete")
try:
    project.files.get(file_path=path, ref="main")
except gitlab.exceptions.GitlabGetError as e:
    print("after delete:", e.response_code, e.error_message)
