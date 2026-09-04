"""Structural checks for coding-team skill packaging and safety routes."""
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ROLES = ("planner", "implementer", "test-writer", "reviewer", "refiner", "orchestrator", "watchdog")


class SkillContractsTest(unittest.TestCase):
    def test_orchestrator_routes_have_attached_sources(self):
        body = (ROOT / "orchestrator.md").read_text()
        links = re.findall(r"\]\((references/[^)]+)\)", body)
        self.assertEqual(len(links), 6)
        self.assertEqual(len(set(links)), 6)
        for link in links:
            with self.subTest(link=link):
                text = (ROOT / "orchestrator" / link).read_text()
                self.assertTrue(text.startswith("## "))
                self.assertIn("Global role, pause, and identity guards still apply", text)
        self.assertNotIn("Forbidden output patterns", body)
        self.assertIn("## Role boundary", body)
        self.assertIn("## Handoff identity and review-loop guard", body)

    def test_shared_reference_links_resolve(self):
        body = (ROOT / "shared-state-ops/SKILL.md").read_text()
        for link in re.findall(r"\]\((references/[^)]+)\)", body):
            with self.subTest(link=link):
                self.assertTrue((ROOT / "shared-state-ops" / link).is_file())
        for name in ROLES[:5]:
            with self.subTest(role=name):
                self.assertIn("references/review-contract.md", (ROOT / f"{name}.md").read_text())

    def test_commit_helpers_have_one_source_for_delivery_roles(self):
        reference = (ROOT / "shared-state-ops/references/branch-sync-and-commits.md").read_text()
        for helper in ("git_commit_clean", "git_push_clean"):
            self.assertIn(helper + "()", reference)
            for name in ("implementer", "test-writer"):
                body = (ROOT / f"{name}.md").read_text()
                self.assertNotIn(helper + "()", body)
                self.assertIn("references/branch-sync-and-commits.md", body)
                self.assertIn('git rev-list --count "origin/$BRANCH..HEAD"', body)

    def test_safety_guards_remain_in_entrypoints(self):
        for name in ROLES:
            with self.subTest(role=name):
                body = (ROOT / f"{name}.md").read_text()
                # Planner uses its existing planning pause rather than repair dispatch.
                if name != "planner":
                    self.assertIn("Decision Needed", body)
        refiner = (ROOT / "refiner.md").read_text()
        self.assertLess(refiner.index("## Pause guard"), refiner.index("## Step 0"))
        self.assertIn('issue status "$MULTICA_ISSUE_ID" in_progress', refiner)
        self.assertIn('issue status "$MULTICA_ISSUE_ID" done', refiner)
        implementer = (ROOT / "implementer.md").read_text()
        self.assertIn("before applying any state patches", implementer)
        self.assertIn("requires_test_writer: true", implementer)

    def test_shared_contract_preserves_quality_and_exception_rules(self):
        text = (ROOT / "shared-state-ops/references/review-contract.md").read_text()
        for marker in ("99%", "coverage is N/A", "per applicable changed production file",
                       "dotnet_test_gate", "Initial independent testing is mandatory",
                       "requires_test_writer: true", "two failed completed repair attempts",
                       "stable IDs", "commit ancestry", "mock", "explicit resolving decision"):
            with self.subTest(marker=marker):
                self.assertIn(marker.lower(), text.lower())


if __name__ == "__main__":
    unittest.main()
