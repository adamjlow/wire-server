#!/usr/bin/env python3
"""Read-only migration record checks. Requires Python 3.10+, no dependencies."""

import argparse
import json
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import urlsplit


STAGES = {
    "registered": {},
    "inventoried": {"inventory": "discovery"},
    "contracted": {"contract": "contract-change"},
    "implementing": {"plan": "discovery"},
    "verified": dict.fromkeys(
        ["parity", "security", "operations", "rollback"], "verification"
    ),
    "available": {"selection": "operations"},
}
TRANSITIONS = {
    "queued": {"queued", "active"},
    "active": {"active", "review", "blocked", "done"},
    "review": {"review", "active", "blocked", "done"},
    "blocked": {"blocked", "queued"},
    "done": {"done"},
}
KINDS = {
    "discovery", "contract-change", "implementation", "verification",
    "operations", "framework",
}
PROTECTED = [
    "services", "libs", "integration", "tools/migration", "AGENTS.md",
    "go/AGENTS.md", ".agent", ".github/workflows/migration.yaml",
    "docs/go-migration/GUARDRAILS.md", "docs/go-migration/SERVICE-LIFECYCLE.md",
    "docs/go-migration/PARITY.md", "docs/go-migration/templates",
    "docs/go-migration/UPSTREAM.md",
]
IDENTIFIER = re.compile(r"[a-z0-9]+(?:-[a-z0-9]+)*\Z")
REVISION = re.compile(r"[0-9a-f]{40}\Z")


class Invalid(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise Invalid(message)


def string(value):
    return isinstance(value, str) and bool(value.strip())


def identifier(value):
    return isinstance(value, str) and IDENTIFIER.fullmatch(value) is not None


def fields(value, expected):
    require(isinstance(value, dict), "expected an object")
    require(set(value) == set(expected.split()), "missing or unknown fields")


def strings(value, nonempty=False):
    require(isinstance(value, list), "expected a list")
    require(all(string(item) for item in value), "expected nonempty strings")
    require(len(value) == len(set(value)), "duplicate list entries")
    require(not nonempty or value, "list must not be empty")


def path(root, value, exists=False):
    require(string(value), "expected a path")
    require(
        not value.startswith("/")
        and not any(c in value for c in "\\*?[]\x00\n\r\t")
        and all(part not in {"", ".", ".."} for part in value.split("/")),
        f"non-canonical relative path: {value!r}",
    )
    target = root
    for part in value.split("/"):
        target = target / part
        require(not target.is_symlink(), f"symlink path forbidden: {value}")
    require(not exists or target.exists(), f"missing path: {value}")
    return target


def artifact(root, value):
    require(string(value), "artifact must be a path or HTTPS URL")
    if value.startswith("https://"):
        url = urlsplit(value)
        require(
            url.hostname and not url.username and not url.password
            and not any(c.isspace() for c in value),
            "artifact URL must have a host and no credentials/whitespace",
        )
    else:
        require(path(root, value, exists=True).is_file(), "artifact must be a file")


def overlaps(left, right):
    return left == right or left.startswith(right + "/") or right.startswith(left + "/")


def no_duplicates(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, f"duplicate JSON key: {key}")
        result[key] = value
    return result


def parse(data):
    def bad_constant(_):
        raise Invalid("non-finite JSON number")

    return json.loads(data, object_pairs_hook=no_duplicates, parse_constant=bad_constant)


def load(root, category):
    directory = path(root, f".migration/{category}", exists=True)
    records = {}
    for entry in sorted(directory.iterdir()):
        require(entry.suffix == ".json", f"unexpected record file: {entry.name}")
        path(root, entry.relative_to(root).as_posix(), exists=True)
        try:
            records[entry.stem] = parse(entry.read_text())
        except (ValueError, OSError) as exc:
            raise Invalid(f"{entry.name}: invalid JSON record ({exc})") from exc
    return records


def service_shape(root, key, record):
    fields(record, "schema_version service reference_path stage oracle_paths gates compatibility")
    require(type(record["schema_version"]) is int and record["schema_version"] == 1,
            "unsupported schema_version")
    require(identifier(key) and record["service"] == key, "service/filename mismatch")
    require(string(record["stage"]) and record["stage"] in STAGES, "unknown stage")
    require(record["reference_path"] == f"services/{key}", "wrong reference_path")
    reference = path(root, record["reference_path"], exists=True)
    require(reference.is_dir(), "missing reference directory")
    strings(record["oracle_paths"], nonempty=True)
    for item in record["oracle_paths"]:
        path(root, item)
    defaults = {
        record["reference_path"], f"docs/go-migration/services/{key}/contract.md",
        f"docs/go-migration/services/{key}/oracle",
    }
    require(defaults <= set(record["oracle_paths"]), "missing default oracle protection")
    require(isinstance(record["gates"], dict), "gates must be an object")
    for value in record["gates"].values():
        strings(value, nonempty=True)
    compatibility = record["compatibility"]
    if compatibility is not None:
        fields(compatibility, "upstream_revision reference_revision candidate_revision verification_task")
        for field in ("upstream_revision", "reference_revision", "candidate_revision"):
            require(isinstance(compatibility[field], str) and REVISION.fullmatch(compatibility[field]),
                    f"compatibility {field} must be a full commit SHA")
        require(identifier(compatibility["verification_task"]), "invalid compatibility task ID")


def task_shape(root, key, record, services):
    fields(record, "schema_version id title service kind status owner branch depends_on "
           "write_paths plan acceptance_criteria evidence review handoff")
    require(type(record["schema_version"]) is int and record["schema_version"] == 1,
            "unsupported schema_version")
    require(identifier(key) and record["id"] == key, "task/filename mismatch")
    require(string(record["title"]), "missing title")
    require(string(record["service"]) and
            (record["service"] in services or record["service"] == "framework"),
            "unregistered service")
    require(string(record["kind"]) and record["kind"] in KINDS, "unknown kind")
    require(string(record["status"]) and record["status"] in TRANSITIONS, "unknown status")
    for claim in ("owner", "branch"):
        if record["status"] == "queued":
            require(record[claim] is None, f"queued task must not have {claim}")
        else:
            require(string(record[claim]), f"claimed task requires {claim}")
    strings(record["depends_on"])
    require(all(identifier(item) for item in record["depends_on"]), "invalid dependency ID")
    strings(record["write_paths"], nonempty=True)
    for item in record["write_paths"]:
        path(root, item)
    require(f".migration/tasks/{key}.json" in record["write_paths"],
            "task must explicitly own its record")
    require(path(root, record["plan"], exists=True).is_file(), "plan must be a file")
    criteria = record["acceptance_criteria"]
    require(isinstance(criteria, list) and criteria, "missing acceptance criteria")
    ids = set()
    for criterion in criteria:
        fields(criterion, "id description required_evidence")
        require(identifier(criterion["id"]) and criterion["id"] not in ids,
                "invalid/duplicate criterion ID")
        ids.add(criterion["id"])
        require(string(criterion["description"]) and string(criterion["required_evidence"]),
                "criterion needs description and required evidence")
    require(isinstance(record["evidence"], list), "evidence must be a list")
    passed = set()
    for evidence in record["evidence"]:
        fields(evidence, "criterion result command revision artifact")
        require(string(evidence["criterion"]) and evidence["criterion"] in ids,
                "unknown evidence criterion")
        require(string(evidence["result"]) and evidence["result"] in {"pass", "fail", "blocked"},
                "unknown evidence result")
        require(string(evidence["command"]), "missing evidence procedure")
        require(isinstance(evidence["revision"], str) and REVISION.fullmatch(evidence["revision"]),
                "evidence revision must be a full commit SHA")
        artifact(root, evidence["artifact"])
        if evidence["result"] == "pass":
            passed.add(evidence["criterion"])
    review = record["review"]
    if review is not None:
        fields(review, "by artifact")
        require(string(review["by"]) and review["by"] != record["owner"],
                "reviewer must differ from owner")
        artifact(root, review["artifact"])
    require(isinstance(record["handoff"], str), "handoff must be text")
    require(record["status"] != "blocked" or string(record["handoff"]),
            "blocked task needs a handoff reason")
    if record["status"] == "done":
        require(passed == ids and all(e["result"] == "pass" for e in record["evidence"]),
                "done task requires passing evidence for every criterion")
        require(review is not None, "done task requires independent review")
    if record["kind"] == "implementation":
        require(record["service"] in services, "implementation needs a registered service")
        if record["status"] in {"active", "review", "done"}:
            require(list(STAGES).index(services[record["service"]]["stage"]) >= 3,
                    "implementation requires the implementing stage")
        protected = PROTECTED + [p for s in services.values() for p in s["oracle_paths"]]
        require(not any(overlaps(w, p) for w in record["write_paths"] for p in protected),
                "implementation write scope overlaps protected oracle")


def validate(root, services, tasks):
    require(services, "service registry must not be empty")
    for key, record in services.items():
        try:
            service_shape(root, key, record)
        except Invalid as exc:
            raise Invalid(f"service {key}: {exc}") from exc
    for key, record in tasks.items():
        try:
            task_shape(root, key, record, services)
        except Invalid as exc:
            raise Invalid(f"task {key}: {exc}") from exc
    for key, task in tasks.items():
        for dependency in task["depends_on"]:
            require(dependency in tasks, f"{key}: missing dependency {dependency}")
            if task["status"] in {"active", "review", "done"}:
                require(tasks[dependency]["status"] == "done", f"{key}: unfinished dependency")
    # Iterative topological check avoids recursion depth limits for long task queues.
    pending = {key: set(t["depends_on"]) for key, t in tasks.items()}
    while pending:
        ready = {key for key, deps in pending.items() if not deps}
        require(ready, "task dependency cycle")
        pending = {key: deps - ready for key, deps in pending.items() if key not in ready}
    active = [t for t in tasks.values() if t["status"] in {"active", "review"}]
    for i, left in enumerate(active):
        for right in active[i + 1:]:
            require(left["branch"] != right["branch"], "active tasks share a branch/worktree")
            require(not any(overlaps(a, b) for a in left["write_paths"] for b in right["write_paths"]),
                    f"write scope conflict: {left['id']} and {right['id']}")
    for key, service in services.items():
        needed = {}
        for stage, gates in STAGES.items():
            needed.update(gates)
            if stage == service["stage"]:
                break
        require(set(service["gates"]) == set(needed), f"{key}: gates do not match stage")
        for gate, kind in needed.items():
            for task_id in service["gates"][gate]:
                task = tasks.get(task_id)
                require(task is not None and task["status"] == "done"
                        and task["service"] == key and task["kind"] == kind,
                        f"{key}: gate {gate} needs completed {kind} task for this service")
        compatibility = service["compatibility"]
        if compatibility is not None:
            task = tasks.get(compatibility["verification_task"])
            require(task is not None and task["status"] == "done"
                    and task["service"] == key and task["kind"] == "verification",
                    f"{key}: compatibility needs a completed verification task for this service")
            require(all(e["revision"] == compatibility["candidate_revision"] for e in task["evidence"]),
                    f"{key}: compatibility evidence does not match candidate revision")


def transitions(old_services, old_tasks, services, tasks):
    for category, old, current in (("service", old_services, services), ("task", old_tasks, tasks)):
        require(set(old) <= set(current), f"{category} records cannot be deleted")
    stages = list(STAGES)
    for key, current in services.items():
        old = old_services.get(key)
        if old is None:
            require(current["stage"] == "registered", "new service must start registered")
            continue
        require(current["reference_path"] == old["reference_path"], "reference path changed")
        require(set(old["oracle_paths"]) <= set(current["oracle_paths"]), "oracle protection removed")
        require(stages.index(current["stage"]) - stages.index(old["stage"]) in {0, 1},
                f"{key}: service must advance one stage at a time")
        require(all(current["gates"].get(gate) == value for gate, value in old["gates"].items()),
                f"{key}: historical gate changed")
        compatibility = current["compatibility"]
        if current["stage"] != old["stage"] and current["stage"] in {"verified", "available"}:
            require(compatibility is not None, f"{key}: stage advancement requires current compatibility")
        previous = old["compatibility"]
        if compatibility is not None and previous is not None and compatibility != previous:
            require(compatibility["verification_task"] != previous["verification_task"],
                    f"{key}: changed compatibility pins require a new verification task")
    locked = ("service", "kind", "owner", "branch", "write_paths", "plan",
              "depends_on", "acceptance_criteria")
    for key, current in tasks.items():
        old = old_tasks.get(key)
        if old is None:
            require(current["status"] == "queued", "new task must start queued")
            continue
        require(current["status"] in TRANSITIONS[old["status"]], f"{key}: illegal task transition")
        if old["status"] == "done":
            require(current == old, f"{key}: completed task is immutable")
        if old["status"] in {"active", "review"}:
            require(all(current[field] == old[field] for field in locked),
                    f"{key}: claimed scope/criteria/identity changed; block and requeue first")


def git(root, *args):
    result = subprocess.run(["git", "-C", str(root), *args], capture_output=True, check=False)
    require(result.returncode == 0, "git read failed; check base commit and checkout history")
    return result.stdout


def baseline(root, revision):
    require(REVISION.fullmatch(revision), "base must be a full lowercase commit SHA")
    git(root, "cat-file", "-e", revision + "^{commit}")
    result = []
    for category in ("services", "tasks"):
        paths = git(root, "ls-tree", "-r", "--name-only", revision, "--", f".migration/{category}")
        records = {}
        for name in paths.decode().splitlines():
            require(name.endswith(".json"), "unexpected baseline record")
            records[Path(name).stem] = parse(git(root, "show", f"{revision}:{name}"))
        result.append(records)
    return result


def scope(root, revision, task):
    # --no-renames includes both the old and new pathname for a move.
    changed = git(root, "diff", "--name-only", "--no-renames", "-z", revision, "--")
    changed += git(root, "ls-files", "--others", "--exclude-standard", "-z")
    for name in filter(None, changed.decode().split("\0")):
        require(any(name == p or name.startswith(p + "/") for p in task["write_paths"]),
                f"{task['id']}: change outside write scope: {name}")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--base", help="full commit SHA for transition validation")
    parser.add_argument("--task", help="also validate working-tree diff against task scope")
    args = parser.parse_args(argv)
    try:
        root = args.root.resolve()
        services, tasks = load(root, "services"), load(root, "tasks")
        validate(root, services, tasks)
        if args.base:
            old_services, old_tasks = baseline(root, args.base)
            transitions(old_services, old_tasks, services, tasks)
        if args.task:
            require(args.base, "--task requires --base")
            require(args.task in tasks, "unknown task")
            require(tasks[args.task]["status"] in {"active", "review", "done"}, "task is not claimed")
            require(args.task in old_tasks and old_tasks[args.task]["status"] in {"active", "review"},
                    "scope check requires a claim already present at base")
            scope(root, args.base, old_tasks[args.task])
        print(f"Migration state valid: {len(services)} services, {len(tasks)} tasks")
        return 0
    except (Invalid, OSError, ValueError, KeyError, TypeError) as exc:
        print(f"Migration check failed: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
