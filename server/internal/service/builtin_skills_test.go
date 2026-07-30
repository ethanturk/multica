package service

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	"gopkg.in/yaml.v3"
)

// Built-in skills are the platform's standard "template" skills. These evals
// pin the template every skill must follow and — crucially — couple each
// skill's documented contract to the real backend behavior it describes, so a
// drift in the source-of-truth (e.g. the mention regex) breaks CI instead of
// silently turning the skill into a lie agents act on.
//
// The evals live in a _test.go file on purpose: anything *inside* a skill
// directory is walked into AgentSkillData.Files and shipped to agent machines
// (see loadBuiltinSkill). Tests must stay out of that payload.

const (
	// maxSkillBodyLines is Anthropic's L2 budget for a SKILL.md body
	// (~5k tokens). Past this, content belongs in one-level-deep supporting
	// files, not the always-loaded body.
	maxSkillBodyLines = 500
	// maxDescriptionChars is the frontmatter description cap — it is the only
	// thing an agent sees when deciding whether to load the skill.
	maxDescriptionChars = 1024
)

// TestBuiltinSkillsConformToTemplate enforces the standard-template invariants
// on every built-in skill, current and future. A new skill that violates the
// shape fails here without anyone having to remember the rules.
func TestBuiltinSkillsConformToTemplate(t *testing.T) {
	skills := loadBuiltinSkills()
	if len(skills) == 0 {
		t.Fatal("no built-in skills loaded; embed or layout is broken")
	}

	for _, skill := range skills {
		t.Run(skill.Name, func(t *testing.T) {
			// The multica- prefix keeps the on-disk slug from colliding with a
			// user-authored workspace skill.
			if !strings.HasPrefix(skill.Name, "multica-") {
				t.Errorf("skill name %q must carry the multica- prefix", skill.Name)
			}

			fm, body, ok := splitFrontmatter(skill.Content)
			if !ok {
				t.Fatalf("SKILL.md must lead with a --- frontmatter block")
			}
			if strings.TrimSpace(fm["name"]) == "" {
				t.Errorf("frontmatter is missing a non-empty name")
			}
			desc := strings.TrimSpace(fm["description"])
			if desc == "" {
				t.Errorf("frontmatter is missing a description (the only thing an agent sees when deciding to load the skill)")
			}
			if len(desc) > maxDescriptionChars {
				t.Errorf("description is %d chars, over the %d cap", len(desc), maxDescriptionChars)
			}
			if n := strings.Count(body, "\n") + 1; n > maxSkillBodyLines {
				t.Errorf("SKILL.md body is %d lines, over the %d-line L2 budget; move detail into one-level-deep supporting files", n, maxSkillBodyLines)
			}

			// Evals must never ride along to agent machines as supporting files.
			for _, f := range skill.Files {
				lower := strings.ToLower(f.Path)
				if strings.Contains(lower, "eval") || strings.HasSuffix(lower, "_test.go") || strings.HasSuffix(lower, "_test.md") {
					t.Errorf("supporting file %q looks like an eval/test; evals belong in _test.go, not the shipped skill payload", f.Path)
				}
			}
		})
	}
}

// TestBuiltinSkillsFrontmatterIsStrictYAML is the regression guard for MUL-3100
// / GitHub #3851: a built-in SKILL.md whose frontmatter is not valid YAML 1.2
// (the canonical break is an unquoted `: ` inside the description) is silently
// dropped by strict runtimes like Codex, so the agent runs without that
// platform-contract skill.
//
// This check is deliberately separate from TestBuiltinSkillsConformToTemplate:
// that test reads the frontmatter through splitFrontmatter, a naive line parser
// that splits on the first ':' and never runs a YAML parse — so it passes even
// on the broken files. Only a real yaml.Unmarshal reproduces what Codex does,
// which is exactly what is needed to catch this class of bug before it ships.
func TestBuiltinSkillsFrontmatterIsStrictYAML(t *testing.T) {
	skills := loadBuiltinSkills()
	if len(skills) == 0 {
		t.Fatal("no built-in skills loaded; embed or layout is broken")
	}

	for _, skill := range skills {
		t.Run(skill.Name, func(t *testing.T) {
			content := skill.Content
			if !strings.HasPrefix(content, "---\n") {
				t.Fatalf("SKILL.md must lead with a --- frontmatter block")
			}
			rest := content[len("---\n"):]
			end := strings.Index(rest, "\n---")
			if end < 0 {
				t.Fatalf("frontmatter has no closing --- delimiter")
			}

			var fm map[string]any
			if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
				t.Fatalf("frontmatter is not valid YAML — a strict runtime (e.g. Codex) "+
					"will drop this skill on load; quote values containing ': ': %v", err)
			}

			if name, ok := fm["name"].(string); !ok || strings.TrimSpace(name) == "" {
				t.Errorf("frontmatter name must parse as a non-empty string, got %#v", fm["name"])
			}
			if desc, ok := fm["description"].(string); !ok || strings.TrimSpace(desc) == "" {
				t.Errorf("frontmatter description must parse as a non-empty string, got %#v", fm["description"])
			}
		})
	}
}

// TestMentioningSkillFollowsContractFrontmatter locks the reference template:
// the mentioning skill is a context-triggered platform-contract skill, so it
// must declare user-invocable:false and fence itself to the multica CLI. New
// contract skills should copy this shape.
func TestMentioningSkillFollowsContractFrontmatter(t *testing.T) {
	skill, ok := findSkill(t, "multica-mentioning")
	if !ok {
		return
	}
	fm, _, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false (a platform-contract skill triggers from context, not a slash command)", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); got != "Bash(multica *)" {
		t.Errorf("allowed-tools = %q, want Bash(multica *) (fence the skill to the CLI it teaches)", got)
	}
}

// TestMentioningSkillTeachesTheParserContract is the eval that gives the skill
// its value: it proves the skill teaches exactly what util.ParseMentions
// enforces. The skill's "Incorrect" examples must parse to nothing (the
// @gpt-boy class of bug: a name where a UUID belongs fails silently), and its
// "Correct" example must parse. If mention.go:16 drifts, this breaks and the
// skill's claims must be re-checked.
func TestMentioningSkillTeachesTheParserContract(t *testing.T) {
	const uuid = "7f3a1b2c-0000-4000-8000-000000000abc"

	cases := []struct {
		name    string
		content string
		want    []util.Mention
	}{
		{
			// Skill: "Writing [@Alice](mention://member/Alice) does NOTHING."
			// 'l'/'i' are not hex, so the id fails to parse — link is dead.
			name:    "name where a uuid belongs is silently dead",
			content: "[@Alice](mention://member/Alice) please review",
			want:    nil,
		},
		{
			// Skill: a bare @name is plain text, nobody is notified.
			name:    "bare @name is plain text",
			content: "@alice please review",
			want:    nil,
		},
		{
			// Skill Step 2: type and id source matched → fires.
			name:    "real uuid with matching type fires",
			content: "[@Alice](mention://member/" + uuid + ") please review",
			want:    []util.Mention{{Type: "member", ID: uuid}},
		},
		{
			// Skill: @all uses the literal `all`, never a UUID.
			name:    "all uses the literal all",
			content: "[@all](mention://all/all) heads up",
			want:    []util.Mention{{Type: "all", ID: "all"}},
		},
		{
			// Skill: "Using the wrong type for an id points at the wrong
			// entity." The link still parses — it just resolves wrong — which
			// is exactly why the skill stresses matching type to id source.
			name:    "wrong type still parses (points at wrong entity)",
			content: "[@Bot](mention://member/" + uuid + ")",
			want:    []util.Mention{{Type: "member", ID: uuid}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := util.ParseMentions(tc.content)
			if len(got) != len(tc.want) {
				t.Fatalf("ParseMentions(%q) = %+v, want %+v", tc.content, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("mention[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestWorkingOnIssuesSkillCoversIssueLoopContracts(t *testing.T) {
	skill, ok := findSkill(t, "multica-working-on-issues")
	if !ok {
		return
	}
	fm, body, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false (issue workflow guidance triggers from context)", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(multica *)") {
		t.Errorf("allowed-tools = %q, want access to the Multica CLI", got)
	}

	// Contract anchors only — exact file:line citations live in the skill's
	// references/source-map.md, not here, so a downstream main merge that
	// shifts a line cannot rot this test into pinning a stale lie.
	mustContain := []string{
		"multica issue pull-requests <issue-id> --output json",
		"Default for code-changing issue work",
		"open or update a PR before posting the final Multica issue comment",
		"This is a default, not",
		"Use a routable issue key in the PR title, body, or branch",
		"include the PR URL when a PR exists",
		"Closes MUL-2759",
		"--status backlog",
		"pr_url",
		"references/working-on-issues-source-map.md",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("working-on-issues skill missing %q", want)
		}
	}

	mustNotContain := []string{
		"Start from the trigger, not from memory",
		"multica issue get <issue-id> --output json",
		"multica issue metadata list <issue-id> --output json",
		"multica issue comment list <issue-id> --thread <trigger-comment-id>",
		"multica issue comment add <issue-id> --parent <trigger-comment-id>",
	}
	for _, forbidden := range mustNotContain {
		if strings.Contains(body, forbidden) {
			t.Errorf("working-on-issues skill duplicates runtime prompt contract %q", forbidden)
		}
	}

	if !skillHasFile(skill, "references/working-on-issues-source-map.md") {
		t.Errorf("working-on-issues skill missing supporting file references/working-on-issues-source-map.md")
	}
}

func TestSkillImportingSkillCoversWorkspaceImportContracts(t *testing.T) {
	skill, ok := findSkill(t, "multica-skill-importing")
	if !ok {
		return
	}
	fm, body, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false (skill import guidance triggers from context)", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(multica *)") {
		t.Errorf("allowed-tools = %q, want access to the Multica CLI", got)
	}

	mustContain := []string{
		"multica skill import --url <url> --output json",
		"/api/skills/import",
		"clawhub.ai",
		"skills.sh",
		"github.com",
		"config.origin",
		"--on-conflict fail",
		"--on-conflict overwrite",
		"--on-conflict rename",
		"--on-conflict skip",
		"status",
		"conflict",
		"skipped",
		"409",
		"existing_skill",
		"id",
		"name",
		"legacy",
		"multica skill list --output json",
		"npx skills add",
		"multica agent skills add <agent-id> --skill-ids <skill-id> --output json",
		"multica agent skills list <agent-id> --output json",
		"replace-all",
		"`set` is the replacement path",
		"references/skill-importing-source-map.md",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("skill-importing skill missing %q", want)
		}
	}

	mustNotContain := []string{
		"multica agent skills set <agent-id> --skill-ids <skill-id>",
		"merge the new skill id with the existing ids",
	}
	for _, forbidden := range mustNotContain {
		if strings.Contains(body, forbidden) {
			t.Errorf("skill-importing skill should not teach stale or destructive binding command %q", forbidden)
		}
	}

	if !skillHasFile(skill, "references/skill-importing-source-map.md") {
		t.Errorf("skill-importing skill missing supporting file references/skill-importing-source-map.md")
	}
}

func TestCreatingAgentsSkillCoversAgentCreationContracts(t *testing.T) {
	skill, ok := findSkill(t, "multica-creating-agents")
	if !ok {
		return
	}
	fm, body, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false (agent creation guidance triggers from context)", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(multica *)") {
		t.Errorf("allowed-tools = %q, want access to the Multica CLI", got)
	}

	mustContain := []string{
		"not a parameter manual",
		"`description` is a catalog summary",
		"`instructions` is the runtime behavior contract",
		"`avatar_url` → a random `emoji:<glyph>`",
		"multica agent create --name <name> --runtime-id <runtime-id>",
		"`model` is a first-class persisted column",
		"custom_env",
		"--custom-env-stdin",
		"--custom-env-file",
		"multica agent skills add <agent-id> --skill-ids <skill-id> --output json",
		"multica agent skills list <agent-id> --output json",
		"multica agent get <agent-id> --output json",
		"255",
		"references/creating-agents-source-map.md",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("creating-agents skill missing %q", want)
		}
	}

	mustNotContain := []string{
		"--from-template",
		"/api/agent-templates",
		"template_slug",
		"curated template",
		"copy this parameter list",
		// De-coaching: this skill states source-backed contracts, it does not
		// teach a generic how-to methodology.
		"Define the job first",
		"Run a low-risk task",
		"Decision flow",
	}
	for _, forbidden := range mustNotContain {
		if strings.Contains(body, forbidden) {
			t.Errorf("creating-agents skill should not teach immature template content or generic how-to coaching %q", forbidden)
		}
	}

	if !skillHasFile(skill, "references/creating-agents-source-map.md") {
		t.Errorf("creating-agents skill missing supporting file references/creating-agents-source-map.md")
	}
}

func TestSquadsSkillCoversLeaderRoutingContract(t *testing.T) {
	skill, ok := findSkill(t, "multica-squads")
	if !ok {
		return
	}
	fm, body, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false (squad guidance triggers from context)", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(multica *)") {
		t.Errorf("allowed-tools = %q, want access to the Multica CLI", got)
	}

	mustContain := []string{
		"A squad is not an agent",
		"squad's `leader_id` agent",
		"squad members are not automatically fanned out",
		"multica squad member set-role",
		"mention://squad/<squad-id>",
		"recording squad activity",
		"references/squad-source-map.md",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("squads skill missing %q", want)
		}
	}

	if !skillHasFile(skill, "references/squad-source-map.md") {
		t.Errorf("squads skill missing supporting file references/squad-source-map.md")
	}
}

func TestAutopilotsSkillCoversDispatchAndSideEffects(t *testing.T) {
	skill, ok := findSkill(t, "multica-autopilots")
	if !ok {
		return
	}
	fm, body, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(multica *)") {
		t.Errorf("allowed-tools = %q, want access to the Multica CLI", got)
	}

	mustContain := []string{
		"An autopilot is not an agent",
		"create_issue",
		"run_only",
		"multica autopilot trigger-add <autopilot-id> --kind schedule",
		"multica autopilot trigger <autopilot-id> --output json",
		"Do not run `trigger`",
		"webhook tokens",
		"{{date}}",
		"squad's leader agent",
		"references/autopilots-source-map.md",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("autopilots skill missing %q", want)
		}
	}
	if !skillHasFile(skill, "references/autopilots-source-map.md") {
		t.Errorf("autopilots skill missing supporting file references/autopilots-source-map.md")
	}
}

func TestRuntimesAndReposSkillCoversClaimAndCheckoutChain(t *testing.T) {
	skill, ok := findSkill(t, "multica-runtimes-and-repos")
	if !ok {
		return
	}
	fm, body, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(multica *)") {
		t.Errorf("allowed-tools = %q, want access to the Multica CLI", got)
	}

	mustContain := []string{
		"agent_task_queue",
		"daemon polls/claims the task",
		"multica runtime list --output json",
		"multica repo checkout <url>",
		"MULTICA_DAEMON_PORT",
		"resource_ref.ref",
		"github_repo",
		"local_directory",
		"Runtime and repo commands affect active agent execution",
		"references/runtimes-and-repos-source-map.md",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("runtimes-and-repos skill missing %q", want)
		}
	}
	if !skillHasFile(skill, "references/runtimes-and-repos-source-map.md") {
		t.Errorf("runtimes-and-repos skill missing supporting file references/runtimes-and-repos-source-map.md")
	}
}

func TestProjectsAndResourcesSkillCoversDurableContext(t *testing.T) {
	skill, ok := findSkill(t, "multica-projects-and-resources")
	if !ok {
		return
	}
	fm, body, _ := splitFrontmatter(skill.Content)

	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false", got)
	}
	if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(multica *)") {
		t.Errorf("allowed-tools = %q, want access to the Multica CLI", got)
	}

	mustContain := []string{
		"Projects are durable context containers",
		".multica/project/resources.json",
		"multica project resource list <project-id> --output json",
		"multica project resource add <project-id> --type github_repo --url <github-url> --output json",
		"multica project resource add <project-id> --type github_repo --url <github-url> --ref <branch-or-sha> --output json",
		"multica project resource add <project-id> --type local_directory",
		"Project resources are durable and affect future tasks",
		"github_repo.resource_ref.url",
		"resource_ref.ref",
		"references/projects-and-resources-source-map.md",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("projects-and-resources skill missing %q", want)
		}
	}
	if !skillHasFile(skill, "references/projects-and-resources-source-map.md") {
		t.Errorf("projects-and-resources skill missing supporting file references/projects-and-resources-source-map.md")
	}
}

func TestBuiltinSkillsUITestContract(t *testing.T) {
	skill, ok := findSkill(t, "multica-ui-testing")
	if !ok {
		return
	}

	fm, body, ok := splitFrontmatter(skill.Content)
	if !ok {
		t.Fatal("UI-testing skill frontmatter is missing")
	}
	strict := strictFrontmatter(t, skill.Content)
	if got := fm["name"]; got != "multica-ui-testing" {
		t.Errorf("name = %q, want multica-ui-testing", got)
	}
	if got := fm["user-invocable"]; got != "false" {
		t.Errorf("user-invocable = %q, want false", got)
	}
	if got, ok := strict["name"].(string); !ok || got != "multica-ui-testing" {
		t.Errorf("strict YAML name = %#v, want multica-ui-testing", strict["name"])
	}
	if got, ok := strict["user-invocable"].(bool); !ok || got {
		t.Errorf("strict YAML user-invocable = %#v, want false", strict["user-invocable"])
	}
	const wantDescription = "Use when an ordinary Multica issue requests a web UI audit, UX review, accessibility check, browser interaction, or Playwright regression coverage."
	if description := fm["description"]; description != wantDescription {
		t.Errorf("description = %q, want trigger-only contract %q", description, wantDescription)
	}

	requireHeadingOrder(t, body,
		"Required tools",
		"Modes",
		"Workflow",
		"Classification",
		"Execution outcomes",
		"Safety and retries",
		"Publication",
		"Source map",
	)

	wantSteps := []string{
		"classify_request",
		"record_baseline",
		"preflight",
		"declare_journeys",
		"navigate_safely",
		"capture_evidence",
		"scan_accessibility",
		"classify_results",
		"write_regression",
		"seal_report",
		"publish",
		"verify_audit",
	}
	if got := markdownNumberedIDs(markdownSection(t, body, "Workflow")); !equalStrings(got, wantSteps) {
		t.Errorf("workflow step IDs = %v, want %v", got, wantSteps)
	}

	tools := markdownTableByKey(t, body, "Required tools", "Role")
	wantTools := map[string]map[string]string{
		"managed_browser": {"Name": "multica-ui-test"},
		"reporter":        {"Name": "ui_test_report", "Input rule": "omit_verdict"},
		"baseline":        {"Name": "repo_facts,diff_summarize"},
		"regression_gate": {"Name": "test_gate"},
	}
	requireTableCases(t, tools, wantTools)

	if !skillHasFile(skill, "references/ui-testing-source-map.md") {
		t.Error("UI-testing skill missing references/ui-testing-source-map.md")
	}
}

func TestBuiltinSkillsUITestScenariosFollowStructuredPolicy(t *testing.T) {
	skill, ok := findSkill(t, "multica-ui-testing")
	if !ok {
		return
	}
	_, body, _ := splitFrontmatter(skill.Content)

	modes := markdownTableByKey(t, body, "Modes", "Mode")
	requireTableCases(t, modes, map[string]map[string]string{
		"audit": {
			"Tracked source":   "forbidden",
			"Persistent tests": "none",
			"Required gate":    "diff_summarize",
		},
		"regression": {
			"Tracked source":   "focused_playwright_only",
			"Persistent tests": "required",
			"Required gate":    "test_gate",
		},
		"both": {
			"Tracked source":   "focused_playwright_only",
			"Persistent tests": "required",
			"Required gate":    "test_gate",
		},
	})

	classifications := markdownTableByKey(t, body, "Classification", "Observation")
	requireTableCases(t, classifications, map[string]map[string]string{
		"assertion_failed": {"Channel": "objective", "Status": "failed"},
		"relevant_first_party_uncaught_console_error":                  {"Channel": "objective", "Status": "failed"},
		"relevant_first_party_request_failure_breaks_or_corrupts_flow": {"Channel": "objective", "Status": "failed"},
		"axe_critical_serious":                                         {"Channel": "objective", "Status": "failed"},
		"regression_test_gate_nonzero":                                 {"Channel": "objective", "Status": "failed"},
		"ux_hierarchy_copy_spacing_discoverability":                    {"Channel": "advisory", "Status": "record"},
		"axe_moderate_minor":                                           {"Channel": "advisory", "Status": "record"},
		"irrelevant_third_party_noise":                                 {"Channel": "advisory_or_omit", "Status": "record_or_omit"},
		"infrastructure_failure":                                       {"Channel": "execution", "Status": "not_run"},
	})

	safety := markdownTableByKey(t, body, "Safety and retries", "Situation")
	requireTableCases(t, safety, map[string]map[string]string{
		"external_navigation": {
			"Action": "prohibit",
		},
		"arbitrary_page_evaluation": {
			"Action": "prohibit",
		},
		"browser_startup_before_interaction": {
			"Action": "bounded_retry",
		},
		"flow_after_product_interaction": {
			"Action":    "no_retry",
			"Condition": "unless_issue_explicitly_idempotent",
		},
	})

	publication := markdownTableByKey(t, body, "Publication", "Outcome")
	requireTableCases(t, publication, map[string]map[string]string{
		"success": {
			"Task response":     "published",
			"Regenerate report": "never",
			"Local artifacts":   "retain",
		},
		"upload_failure": {
			"Task response":     "degraded",
			"Regenerate report": "never",
			"Local artifacts":   "retain",
		},
	})
}

func TestBuiltinSkillsUITestBlockedExecutionPrecedesObjectiveFailure(t *testing.T) {
	skill, ok := findSkill(t, "multica-ui-testing")
	if !ok {
		return
	}
	_, body, _ := splitFrontmatter(skill.Content)

	classifications := markdownTableByKey(t, body, "Classification", "Observation")
	if got := classifications["assertion_failed"]; got["Channel"] != "objective" || got["Status"] != "failed" {
		t.Fatalf("assertion failure policy = %v, want objective/failed", got)
	}
	safety := markdownTableByKey(t, body, "Safety and retries", "Situation")
	if got := safety["external_navigation"]; got["Action"] != "prohibit" {
		t.Fatalf("external navigation policy = %v, want prohibit", got)
	}

	outcomes := markdownTableByKey(t, body, "Execution outcomes", "Case")
	requireTableCases(t, outcomes, map[string]map[string]string{
		"completed_failed": {
			"Execution status":   "completed",
			"Objective failures": "one_or_more",
			"Verdict":            "fail",
		},
		"completed_passed": {
			"Execution status":   "completed",
			"Objective failures": "none",
			"Verdict":            "pass",
		},
		"infrastructure_error": {
			"Execution status":   "infrastructure_error",
			"Objective failures": "ignored",
			"Verdict":            "not_run",
		},
		"blocked": {
			"Execution status":   "blocked",
			"Objective failures": "ignored",
			"Verdict":            "not_run",
		},
	})

	// Scenario: an assertion failed, then external navigation hit the policy
	// boundary. The terminal blocked execution status overrides the earlier
	// objective failure when selecting the report verdict.
	if got := uiTestVerdictFromPolicy(t, outcomes, "blocked", true); got != "not_run" {
		t.Errorf("blocked execution with prior objective failure = %q, want not_run", got)
	}
}

func TestBuiltinSkillsUITestPublicationCommandShape(t *testing.T) {
	skill, ok := findSkill(t, "multica-ui-testing")
	if !ok {
		return
	}
	_, body, _ := splitFrontmatter(skill.Content)
	command := markdownFencedBlock(t, markdownSection(t, body, "Publication"), "bash")
	command = strings.ReplaceAll(command, "\\\n", " ")
	command = strings.NewReplacer(`"`, "", `'`, "").Replace(command)
	fields := strings.Fields(command)

	start := stringIndex(fields, "multica")
	if start < 0 {
		t.Fatal("publication block has no multica command")
	}
	fields = fields[start:]
	wantPrefix := []string{"multica", "issue", "comment", "add", "$ISSUE_ID"}
	if len(fields) < len(wantPrefix) || !equalStrings(fields[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("publication command prefix = %v, want %v", fields, wantPrefix)
	}
	if got := commandFlagValues(fields, "--content-file"); !equalStrings(got, []string{
		".multica/artifacts/ui-test/$MULTICA_TASK_ID/comment.md",
	}) {
		t.Errorf("--content-file values = %v", got)
	}
	if got := commandFlagValues(fields, "--attachment"); !equalStrings(got, []string{
		".multica/artifacts/ui-test/$MULTICA_TASK_ID/report.json",
		".multica/artifacts/ui-test/$MULTICA_TASK_ID/report.md",
		".multica/artifacts/ui-test/$MULTICA_TASK_ID/artifact-manifest.json",
	}) {
		t.Errorf("--attachment values = %v", got)
	}

	attachments := markdownTableByKey(t, body, "Publication attachments", "Artifact")
	requireTableCases(t, attachments, map[string]map[string]string{
		"comment.md":             {"Flag": "--content-file", "Policy": "required"},
		"report.json":            {"Flag": "--attachment", "Policy": "required"},
		"report.md":              {"Flag": "--attachment", "Policy": "required"},
		"artifact-manifest.json": {"Flag": "--attachment", "Policy": "required"},
		"sealed_png_text_json":   {"Flag": "--attachment", "Policy": "relevant_only"},
		"trace_zip":              {"Flag": "none", "Policy": "rejected_v1"},
	})
}

func TestBuiltinSkillsUITestSourceMapLinksResolve(t *testing.T) {
	skill, ok := findSkill(t, "multica-ui-testing")
	if !ok {
		return
	}
	sourceMap := skillFileContent(t, skill, "references/ui-testing-source-map.md")
	sources := markdownTableByKey(t, sourceMap, "Sources", "Contract")
	requireTableCases(t, sources, map[string]map[string]string{
		"managed_runtime":       {"Target": "server/pkg/uitest/"},
		"daemon_injection":      {"Target": "server/internal/daemon/ui_test_inject.go"},
		"deterministic_report":  {"Target": "server/pkg/dettools/tool_ui_test_report.go"},
		"ui_test_cli":           {"Target": "server/cmd/multica/cmd_uitest.go"},
		"issue_publication":     {"Target": "server/cmd/multica/cmd_issue.go"},
		"repository_manifest":   {"Target": ".multica/ui-test.json"},
		"playwright_regression": {"Target": "playwright.config.ts"},
	})

	root := repositoryRoot(t)
	sourceMapPath := filepath.Join(root,
		"server/internal/service/builtin_skills/multica-ui-testing/references/ui-testing-source-map.md")
	for _, target := range markdownLinkTargets(sourceMap) {
		if strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
			continue
		}
		resolved := filepath.Clean(filepath.Join(filepath.Dir(sourceMapPath), filepath.FromSlash(target)))
		if _, err := os.Stat(resolved); err != nil {
			t.Errorf("source-map link %q resolves to missing path %q: %v", target, resolved, err)
		}
	}
}

func TestBuiltinSkillsUITestDocumentationStructure(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "docs/ui-testing.md"))
	if err != nil {
		t.Fatalf("read docs/ui-testing.md: %v", err)
	}
	doc := string(data)
	requireHeadingOrder(t, doc,
		"Install and check status",
		"Repository contract",
		"Ordinary issue examples",
		"Verdicts and findings",
		"Artifacts and publication",
		"Security limits",
		"Readiness and troubleshooting",
	)

	examples := markdownTableByKey(t, doc, "Ordinary issue examples", "Mode")
	requireTableCases(t, examples, map[string]map[string]string{
		"audit":      {"Source changes": "none"},
		"regression": {"Source changes": "focused_playwright"},
		"both":       {"Source changes": "focused_playwright"},
	})
	verdicts := markdownTableByKey(t, doc, "Verdicts and findings", "Result")
	requireTableCases(t, verdicts, map[string]map[string]string{
		"objective_failure":    {"Verdict effect": "fail"},
		"advisory_finding":     {"Verdict effect": "none"},
		"infrastructure_error": {"Verdict effect": "not_run"},
		"blocked":              {"Verdict effect": "not_run"},
	})
	evidence := markdownTableByKey(t, doc, "Evidence compatibility", "Evidence")
	requireTableCases(t, evidence, map[string]map[string]string{
		"png":       {"V1 report input": "accepted"},
		"text_json": {"V1 report input": "accepted"},
		"trace_zip": {"V1 report input": "rejected"},
	})
}

func findSkill(t *testing.T, name string) (AgentSkillData, bool) {
	t.Helper()
	for _, s := range loadBuiltinSkills() {
		if s.Name == name {
			return s, true
		}
	}
	t.Errorf("built-in skill %q not found", name)
	return AgentSkillData{}, false
}

func skillHasFile(skill AgentSkillData, path string) bool {
	for _, f := range skill.Files {
		if f.Path == path {
			return true
		}
	}
	return false
}

func skillFileContent(t *testing.T, skill AgentSkillData, path string) string {
	t.Helper()
	for _, file := range skill.Files {
		if file.Path == path {
			return file.Content
		}
	}
	t.Fatalf("skill %q missing supporting file %q", skill.Name, path)
	return ""
}

func strictFrontmatter(t *testing.T, content string) map[string]any {
	t.Helper()
	if !strings.HasPrefix(content, "---\n") {
		t.Fatal("SKILL.md must lead with YAML frontmatter")
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		t.Fatal("SKILL.md frontmatter has no closing delimiter")
	}
	var frontmatter map[string]any
	if err := yaml.Unmarshal([]byte(rest[:end]), &frontmatter); err != nil {
		t.Fatalf("SKILL.md frontmatter is not strict YAML: %v", err)
	}
	return frontmatter
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	current, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "CLAUDE.md")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatal("repository root not found")
		}
		current = parent
	}
}

func requireHeadingOrder(t *testing.T, markdown string, headings ...string) {
	t.Helper()
	offset := 0
	for _, heading := range headings {
		needle := "## " + heading
		next := strings.Index(markdown[offset:], needle)
		if next < 0 {
			t.Errorf("missing ordered heading %q", heading)
			continue
		}
		offset += next + len(needle)
	}
}

func markdownSection(t *testing.T, markdown, heading string) string {
	t.Helper()
	needle := "## " + heading
	start := strings.Index(markdown, needle)
	if start < 0 {
		t.Fatalf("missing heading %q", heading)
	}
	start += len(needle)
	rest := markdown[start:]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

func markdownNumberedIDs(section string) []string {
	pattern := regexp.MustCompile(`(?m)^\d+\.\s+` + "`" + `([a-z0-9_]+)` + "`")
	matches := pattern.FindAllStringSubmatch(section, -1)
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match[1])
	}
	return ids
}

func markdownTableByKey(t *testing.T, markdown, heading, key string) map[string]map[string]string {
	t.Helper()
	section := markdownSection(t, markdown, heading)
	lines := strings.Split(section, "\n")
	for index := 0; index+1 < len(lines); index++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[index]), "|") ||
			!strings.HasPrefix(strings.TrimSpace(lines[index+1]), "|") {
			continue
		}
		headers := markdownRow(lines[index])
		if !isMarkdownSeparator(markdownRow(lines[index+1])) {
			continue
		}
		headerSet := make(map[string]bool, len(headers))
		for _, header := range headers {
			if header == "" || headerSet[header] {
				t.Fatalf("table %q has blank or duplicate header %q", heading, header)
			}
			headerSet[header] = true
		}
		if !headerSet[key] {
			t.Fatalf("table %q is missing key column %q", heading, key)
		}
		rows := make(map[string]map[string]string)
		for _, line := range lines[index+2:] {
			if !strings.HasPrefix(strings.TrimSpace(line), "|") {
				break
			}
			values := markdownRow(line)
			if len(values) != len(headers) {
				t.Fatalf("table %q row has %d columns, want %d: %q", heading, len(values), len(headers), line)
			}
			row := make(map[string]string, len(headers))
			for column := range headers {
				row[headers[column]] = values[column]
			}
			rowKey := row[key]
			if rowKey == "" {
				t.Fatalf("table %q has an empty %q key", heading, key)
			}
			if _, exists := rows[rowKey]; exists {
				t.Fatalf("table %q has duplicate %q key %q", heading, key, rowKey)
			}
			rows[rowKey] = row
		}
		if len(rows) == 0 {
			t.Fatalf("table %q has no data rows", heading)
		}
		return rows
	}
	t.Fatalf("heading %q has no Markdown table", heading)
	return nil
}

func markdownRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	cells := strings.Split(line, "|")
	for index := range cells {
		cells[index] = strings.Trim(strings.TrimSpace(cells[index]), "`")
	}
	return cells
}

func isMarkdownSeparator(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		cell = strings.Trim(strings.TrimSpace(cell), ":")
		if len(cell) < 3 || strings.Trim(cell, "-") != "" {
			return false
		}
	}
	return true
}

func requireTableCases(t *testing.T, rows, cases map[string]map[string]string) {
	t.Helper()
	for key, expected := range cases {
		row, ok := rows[key]
		if !ok {
			t.Errorf("table row %q is missing", key)
			continue
		}
		for column, want := range expected {
			if got := row[column]; got != want {
				t.Errorf("table[%q][%q] = %q, want %q", key, column, got, want)
			}
		}
	}
}

func uiTestVerdictFromPolicy(
	t *testing.T,
	outcomes map[string]map[string]string,
	executionStatus string,
	hasObjectiveFailure bool,
) string {
	t.Helper()
	key := executionStatus
	if executionStatus == "completed" {
		if hasObjectiveFailure {
			key = "completed_failed"
		} else {
			key = "completed_passed"
		}
	}
	row, ok := outcomes[key]
	if !ok {
		t.Fatalf("execution outcome %q is missing", key)
	}
	if got := row["Execution status"]; got != executionStatus {
		t.Fatalf("execution outcome %q status = %q, want %q", key, got, executionStatus)
	}
	switch objectiveRule := row["Objective failures"]; objectiveRule {
	case "none":
		if hasObjectiveFailure {
			t.Fatalf("execution outcome %q requires no objective failures", key)
		}
	case "one_or_more":
		if !hasObjectiveFailure {
			t.Fatalf("execution outcome %q requires an objective failure", key)
		}
	case "ignored":
	default:
		t.Fatalf("execution outcome %q has unknown objective rule %q", key, objectiveRule)
	}
	return row["Verdict"]
}

func markdownFencedBlock(t *testing.T, section, language string) string {
	t.Helper()
	startMarker := "```" + language + "\n"
	start := strings.Index(section, startMarker)
	if start < 0 {
		t.Fatalf("missing %s fenced block", language)
	}
	rest := section[start+len(startMarker):]
	end := strings.Index(rest, "\n```")
	if end < 0 {
		t.Fatalf("unterminated %s fenced block", language)
	}
	return rest[:end]
}

func markdownLinkTargets(markdown string) []string {
	pattern := regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)
	matches := pattern.FindAllStringSubmatch(markdown, -1)
	targets := make([]string, 0, len(matches))
	for _, match := range matches {
		targets = append(targets, match[1])
	}
	return targets
}

func commandFlagValues(fields []string, flag string) []string {
	var values []string
	for index := 0; index+1 < len(fields); index++ {
		if fields[index] == flag {
			values = append(values, fields[index+1])
		}
	}
	return values
}

func stringIndex(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// splitFrontmatter returns the top-level scalar keys of a leading YAML
// frontmatter block, the body after it, and whether a block was found. It only
// understands flat `key: value` lines — enough for the template's frontmatter.
func splitFrontmatter(content string) (map[string]string, string, bool) {
	if !strings.HasPrefix(content, "---\n") {
		return nil, content, false
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, content, false
	}
	block := rest[:end]
	body := rest[end:]
	if nl := strings.Index(body, "\n"); nl >= 0 {
		body = body[nl+1:] // drop the closing --- line
	}

	fm := make(map[string]string)
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue // nested value; the template uses only flat scalars
		}
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fm[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(val), `"'`)
	}
	return fm, body, true
}
