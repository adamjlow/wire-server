"""Tests for migration safety checks; no services, networks or integration tests."""

from copy import deepcopy
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest


SPEC = importlib.util.spec_from_file_location("migration_check", Path(__file__).with_name("check.py"))
check = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(check)
REPO = Path(__file__).resolve().parents[2]


class ValidationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "services/proxy").mkdir(parents=True)
        (self.root / "plan.md").write_text("Executable test plan\n")
        (self.root / "report.txt").write_text("Synthetic test evidence\n")
        self.service = json.loads((REPO / ".migration/services/proxy.json").read_text())
        self.task = json.loads((REPO / ".migration/tasks/proxy-inventory.json").read_text())
        self.task["plan"] = "plan.md"
        self.services = {"proxy": self.service}
        self.tasks = {self.task["id"]: self.task}

    def validate(self):
        check.validate(self.root, self.services, self.tasks)

    def reject(self, pattern):
        with self.assertRaisesRegex(check.Invalid, pattern):
            self.validate()

    def claim(self, task=None):
        task = self.task if task is None else task
        task.update(status="active", owner="test-agent", branch=task["id"])

    def done(self, task=None):
        task = self.task if task is None else task
        self.claim(task)
        task.update(status="done", review={"by": "independent-reviewer", "artifact": "report.txt"})
        task["evidence"] = [
            dict(criterion=c["id"], result="pass", command="manual source review",
                 revision="a" * 40, artifact="report.txt")
            for c in task["acceptance_criteria"]
        ]

    def add_task(self, name):
        task = deepcopy(self.task)
        task.update(id=name, write_paths=[f".migration/tasks/{name}.json"])
        self.tasks[name] = task
        return task

    def verify_compatibility(self, name="baseline-verification"):
        task = self.add_task(name)
        task["kind"] = "verification"
        self.done(task)
        self.service["compatibility"] = dict(
            upstream_revision="b" * 40, reference_revision="c" * 40,
            candidate_revision="a" * 40, verification_task=name,
        )

    def test_seed_and_completed_discovery_gate(self):
        self.validate()
        self.done()
        self.service.update(stage="inventoried", gates={"inventory": [self.task["id"]]})
        self.validate()

    def test_schema_rejects_unknown_fields_and_boolean_version(self):
        self.task["surprise"] = True
        self.reject("unknown fields")
        del self.task["surprise"]
        self.task["schema_version"] = True
        self.reject("schema_version")

    def test_unknown_service_status_and_kind(self):
        for key, value in [("service", "absent"), ("status", "finished"), ("kind", "rewrite")]:
            with self.subTest(key=key):
                original = self.task[key]
                self.task[key] = value
                self.reject("unregistered|unknown")
                self.task[key] = original

    def test_claim_required_and_queued_has_no_owner(self):
        self.task["owner"] = "agent"
        self.reject("queued")
        self.task["status"] = "active"
        self.reject("branch")

    def test_paths_reject_traversal_absolute_globs_and_noncanonical_forms(self):
        for bad in ["../outside", "/etc/passwd", "go/*", "go/../services", "go//x", "go/", ".", "go\\x"]:
            with self.subTest(path=bad):
                self.task["write_paths"][-1] = bad
                self.reject("relative path")

    def test_symlink_parent_cannot_escape_scope_or_plan(self):
        (self.root / "alias").symlink_to(self.root / "services", target_is_directory=True)
        self.task["write_paths"].append("alias/proxy/output")
        self.reject("symlink")

    def test_missing_plan_and_record_ownership(self):
        self.task["plan"] = "absent.md"
        self.reject("missing path")
        self.task["plan"] = "plan.md"
        self.task["write_paths"] = ["go/services/proxy"]
        self.reject("own its record")

    def test_dependencies_missing_cycle_and_unfinished(self):
        self.task["depends_on"] = ["missing"]
        self.reject("missing dependency")
        self.task["depends_on"] = [self.task["id"]]
        self.reject("cycle")
        other = self.add_task("other")
        other["depends_on"] = []
        self.task["depends_on"] = ["other"]
        self.claim()
        self.reject("unfinished dependency")

    def test_active_and_review_ownership_conflicts(self):
        other = self.add_task("other")
        self.claim()
        self.claim(other)
        other["status"] = "review"
        self.task["write_paths"].append("go/services/proxy")
        other["write_paths"].append("go/services/proxy/main.go")
        self.reject("scope conflict")
        other["write_paths"][-1] = "go/services/proxy-other"
        self.validate()
        other["branch"] = self.task["branch"]
        self.reject("share a branch")

    def test_blocked_releases_scope_but_requires_handoff(self):
        other = self.add_task("other")
        self.claim()
        self.claim(other)
        other.update(status="blocked", handoff="")
        self.reject("handoff")
        other["handoff"] = "Await source review before requeueing"
        other["write_paths"].append(self.task["write_paths"][-1])
        self.validate()

    def test_implementation_cannot_claim_reference_fixtures_or_policy(self):
        self.task["kind"] = "implementation"
        for protected in ["services/proxy/src", "libs/wire-api/test/golden", "integration",
                          "docs/go-migration/services/proxy/contract.md", "tools/migration",
                          "AGENTS.md", "docs/go-migration", "go/AGENTS.md"]:
            with self.subTest(path=protected):
                self.task["write_paths"][-1] = protected
                self.reject("protected oracle")

    def test_implementation_requires_contract_and_plan_gate(self):
        self.task["kind"] = "implementation"
        self.claim()
        self.reject("implementing stage")

    def test_done_requires_complete_passing_evidence_and_review(self):
        self.done()
        self.task["evidence"].pop()
        self.reject("every criterion")
        self.done()
        self.task["evidence"][0]["result"] = "blocked"
        self.reject("every criterion")
        self.done()
        self.task["review"] = None
        self.reject("independent review")
        self.done()
        self.task["review"]["by"] = self.task["owner"]
        self.reject("differ from owner")

    def test_evidence_revision_artifact_and_criterion_validation(self):
        for field, value, expected in [
            ("revision", "HEAD", "full commit SHA"),
            ("criterion", "absent", "unknown evidence"),
            ("artifact", "https://user:password@example.invalid/log", "credentials"),
            ("artifact", "http://example.invalid/log", "relative path"),
            ("artifact", "missing.txt", "missing path"),
        ]:
            with self.subTest(field=field):
                self.done()
                self.task["evidence"][0][field] = value
                self.reject(expected)

    def test_done_command_is_data_not_executed(self):
        self.done()
        self.task["evidence"][0]["command"] = "$(touch should-not-exist); exit 1"
        self.validate()
        self.assertFalse((self.root / "should-not-exist").exists())

    def test_gate_requires_matching_completed_service_task_kind(self):
        self.service.update(stage="inventoried", gates={"inventory": [self.task["id"]]})
        self.reject("completed discovery")
        self.done()
        self.task["kind"] = "verification"
        self.reject("completed discovery")
        self.task["kind"] = "discovery"
        self.task["service"] = "framework"
        self.reject("this service")

    def test_exact_cumulative_gates_and_default_protection(self):
        self.service["stage"] = "verified"
        self.reject("gates do not match")
        self.service["stage"] = "registered"
        self.service["oracle_paths"].pop()
        self.reject("default oracle")

    def test_parser_rejects_duplicate_keys_and_nonfinite_numbers(self):
        for value in ['{"a":1,"a":2}', '{"a":NaN}', '{"a":Infinity}']:
            with self.assertRaises(check.Invalid):
                check.parse(value)

    def test_full_lifecycle_can_be_evidenced(self):
        previous = deepcopy(self.services)
        for stage, gates in check.STAGES.items():
            self.service["stage"] = stage
            for gate, kind in gates.items():
                task = self.add_task("gate-" + gate)
                task["kind"] = kind
                self.done(task)
                self.service["gates"][gate] = [task["id"]]
            if stage == "verified":
                self.verify_compatibility()
            self.validate()
            check.transitions(previous, self.tasks, self.services, self.tasks)
            previous = deepcopy(self.services)

    def test_global_cutover_and_retirement_are_not_stages(self):
        for stage in ("cutover", "retired"):
            self.service["stage"] = stage
            self.reject("unknown stage")

    def test_haskell_reference_required_even_when_go_is_available(self):
        self.service["stage"] = "available"
        (self.root / "services/proxy").rmdir()
        self.reject("missing path")

    def test_compatibility_requires_full_pins_and_matching_done_task(self):
        self.verify_compatibility()
        self.validate()
        for field in ("upstream_revision", "reference_revision", "candidate_revision"):
            with self.subTest(field=field):
                original = self.service["compatibility"][field]
                self.service["compatibility"][field] = "HEAD"
                self.reject("full commit SHA")
                self.service["compatibility"][field] = original
        self.service["compatibility"]["verification_task"] = "absent"
        self.reject("completed verification")
        self.verify_compatibility()
        task = self.tasks["baseline-verification"]
        task["service"] = "framework"
        self.reject("for this service")
        task["service"] = "proxy"
        task["kind"] = "discovery"
        self.reject("completed verification")
        task["kind"] = "verification"
        task["status"] = "review"
        self.reject("completed verification")

    def test_compatibility_evidence_must_match_candidate(self):
        self.verify_compatibility()
        self.service["compatibility"]["candidate_revision"] = "d" * 40
        self.reject("does not match candidate")

    def test_verified_and_available_advancement_require_current_compatibility(self):
        for old_stage, new_stage in (("implementing", "verified"), ("verified", "available")):
            self.service["stage"] = old_stage
            old = deepcopy(self.services)
            self.service["stage"] = new_stage
            with self.assertRaisesRegex(check.Invalid, "requires current compatibility"):
                check.transitions(old, self.tasks, self.services, self.tasks)

    def test_upstream_sync_can_clear_and_renew_claim_without_rewriting_gates(self):
        self.test_full_lifecycle_can_be_evidenced()
        old = deepcopy(self.services)
        gates = deepcopy(self.service["gates"])
        self.service["compatibility"] = None
        self.validate()
        check.transitions(old, self.tasks, self.services, self.tasks)
        cleared = deepcopy(self.services)
        self.verify_compatibility("updated-baseline")
        self.service["compatibility"]["upstream_revision"] = "d" * 40
        self.validate()
        check.transitions(cleared, self.tasks, self.services, self.tasks)
        self.assertEqual(self.service["gates"], gates)
        self.assertEqual(self.service["stage"], "available")

    def test_changed_upstream_or_reference_cannot_relabel_old_verification(self):
        self.verify_compatibility()
        for field in ("upstream_revision", "reference_revision"):
            with self.subTest(field=field):
                old = deepcopy(self.services)
                self.service["compatibility"][field] = "d" * 40
                with self.assertRaisesRegex(check.Invalid, "new verification task"):
                    check.transitions(old, self.tasks, self.services, self.tasks)
                self.service["compatibility"][field] = old["proxy"]["compatibility"][field]

    def test_transition_rejects_skipped_stage_removed_oracle_and_record(self):
        old = deepcopy(self.services)
        self.service["stage"] = "contracted"
        with self.assertRaisesRegex(check.Invalid, "one stage"):
            check.transitions(old, {}, self.services, self.tasks)
        self.service["stage"] = "registered"
        self.service["oracle_paths"].pop()
        with self.assertRaisesRegex(check.Invalid, "protection removed"):
            check.transitions(old, {}, self.services, self.tasks)
        with self.assertRaisesRegex(check.Invalid, "deleted"):
            check.transitions(old, {}, {}, {})

    def test_transition_rejects_new_done_and_claim_mutation(self):
        self.done()
        with self.assertRaisesRegex(check.Invalid, "start queued"):
            check.transitions({}, {}, self.services, self.tasks)
        self.claim()
        old = deepcopy(self.tasks)
        self.task["write_paths"].append("shared")
        with self.assertRaisesRegex(check.Invalid, "claimed scope"):
            check.transitions(self.services, old, self.services, self.tasks)

    def test_done_immutable_and_illegal_reopen(self):
        self.done()
        old = deepcopy(self.tasks)
        self.task["handoff"] = "changed"
        with self.assertRaisesRegex(check.Invalid, "immutable"):
            check.transitions(self.services, old, self.services, self.tasks)
        self.task["status"] = "active"
        with self.assertRaisesRegex(check.Invalid, "illegal task"):
            check.transitions(self.services, old, self.services, self.tasks)

    def test_historical_gate_cannot_be_replaced(self):
        self.done()
        self.service.update(stage="inventoried", gates={"inventory": [self.task["id"]]})
        old = deepcopy(self.services)
        self.service["gates"]["inventory"] = ["other"]
        with self.assertRaisesRegex(check.Invalid, "historical gate"):
            check.transitions(old, self.tasks, self.services, self.tasks)

    def git(self, *args):
        return subprocess.check_output(["git", "-C", str(self.root), *args], stderr=subprocess.DEVNULL)

    def test_git_diff_catches_untracked_staged_deleted_and_renamed_paths(self):
        self.git("init", "-q")
        self.git("add", ".")
        self.git("-c", "user.name=Synthetic Test", "-c", "user.email=test@example.invalid",
                 "commit", "-qm", "fixture")
        base = self.git("rev-parse", "HEAD").decode().strip()
        self.task["write_paths"] = ["plan.md"]
        (self.root / "plan.md").write_text("allowed\n")
        check.scope(self.root, base, self.task)
        (self.root / "extra").write_text("untracked\n")
        with self.assertRaisesRegex(check.Invalid, "outside write scope"):
            check.scope(self.root, base, self.task)
        self.git("add", "extra")
        with self.assertRaisesRegex(check.Invalid, "outside write scope"):
            check.scope(self.root, base, self.task)
        self.git("rm", "--cached", "extra")
        (self.root / "extra").unlink()
        (self.root / "report.txt").unlink()
        with self.assertRaisesRegex(check.Invalid, "report.txt"):
            check.scope(self.root, base, self.task)
        self.git("restore", "report.txt")
        self.git("mv", "report.txt", "moved.txt")
        self.task["write_paths"].append("moved.txt")
        with self.assertRaisesRegex(check.Invalid, "report.txt"):
            check.scope(self.root, base, self.task)

    def test_base_must_be_commit_sha_not_option_or_revision_expression(self):
        for bad in ["HEAD", "--help", "a" * 39, "a" * 40 + "~1"]:
            with self.assertRaisesRegex(check.Invalid, "full lowercase commit"):
                check.baseline(self.root, bad)


if __name__ == "__main__":
    unittest.main()
