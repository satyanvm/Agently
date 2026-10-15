package services

import (
	"errors"
	"testing"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
)

func preflightGraph() []domain.GraphNode {
	return []domain.GraphNode{
		{Key: "start", Type: "trigger.manual"},
		{Key: "think", Name: "Think", DependsOn: []string{"start"}}, // prompt-compiled agent.llm
		{Key: "issues", Name: "List issues", Type: "github.listIssues", DependsOn: []string{"start"}},
		{Key: "repo", Name: "Get repo", Type: "github.getRepo", DependsOn: []string{"start"}},
		{Key: "post", Name: "Post", Type: "pieces.slack.custom_api_call", DependsOn: []string{"issues"}},
		{Key: "hn", Name: "HN", Type: "pieces.hackernews.fetch_top_stories", DependsOn: []string{"start"}},
	}
}

func setupPreflight(t *testing.T) *Platform {
	t.Helper()
	for _, k := range []string{"GITHUB_TOKEN", "AP_SLACK_AUTH", "RUN_CREDENTIAL_CHECK"} {
		t.Setenv(k, "")
	}
	p := newTestPlatform()
	if _, err := p.Workflows.SaveGraph("ai-digest", preflightGraph()); err != nil {
		t.Fatalf("save graph: %v", err)
	}
	return p
}

func accountByType(pf RunPreflight, typ string) *PreflightAccount {
	for i := range pf.Accounts {
		if pf.Accounts[i].CredentialType == typ {
			return &pf.Accounts[i]
		}
	}
	return nil
}

func TestPreflightListsAccountsToConnect(t *testing.T) {
	p := setupPreflight(t)
	pf, err := p.Runs.Preflight("ai-digest")
	if err != nil {
		t.Fatal(err)
	}
	if pf.Ready || len(pf.Accounts) != 2 {
		t.Fatalf("want two accounts missing, got %+v", pf)
	}
	gh := accountByType(pf, "github")
	if gh == nil || gh.Label != "GitHub" || len(gh.Nodes) != 2 || gh.EnvKeys[0] != "GITHUB_TOKEN" {
		t.Fatalf("github account: %+v", gh)
	}
	if gh.Nodes[0].Problem != ProblemNotConnected {
		t.Fatalf("problem: %+v", gh.Nodes[0])
	}
	slack := accountByType(pf, "pieces.slack")
	if slack == nil || slack.AuthType != "oauth2" || slack.EnvKeys[0] != "AP_SLACK_AUTH" {
		t.Fatalf("slack account: %+v", slack)
	}
}

func TestPreflightEnvFallbackSatisfies(t *testing.T) {
	p := setupPreflight(t)
	t.Setenv("GITHUB_TOKEN", "ghp_x")
	t.Setenv("AP_SLACK_AUTH", "xoxb")
	pf, _ := p.Runs.Preflight("ai-digest")
	if !pf.Ready {
		t.Fatalf("env should satisfy every node: %+v", pf)
	}
}

func TestManualLaunchRefusedUntilConnected(t *testing.T) {
	p := setupPreflight(t)
	_, err := p.Runs.Launch("ai-digest", validate.LaunchRunInput{Trigger: domain.TriggerManual})
	var de *domain.DomainError
	if !errors.As(err, &de) || de.Code != domain.CodeCredentialsRequired || len(de.Details) != 3 {
		t.Fatalf("want credentials_required with 3 details, got %v", err)
	}
	if len(p.Repos.Runs.All()) != 0 {
		t.Fatal("a refused launch must not create a run")
	}
	// The escape hatch launches anyway.
	detail, err := p.Runs.Launch("ai-digest", validate.LaunchRunInput{Trigger: domain.TriggerManual, SkipCredentialCheck: true})
	if err != nil || detail.Status != domain.RunQueued {
		t.Fatalf("skip: %v %v", err, detail.Status)
	}
}

func TestScheduledLaunchRecordsFailedRun(t *testing.T) {
	p := setupPreflight(t)
	detail, err := p.Runs.Launch("ai-digest", validate.LaunchRunInput{Trigger: domain.TriggerSchedule})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != domain.RunFailed || detail.Error == nil || detail.FinishedAt == nil {
		t.Fatalf("want a failed run with a reason, got %+v", detail.Run)
	}
}

func TestCheckCanBeTurnedOff(t *testing.T) {
	p := setupPreflight(t)
	t.Setenv("RUN_CREDENTIAL_CHECK", "off")
	if _, err := p.Runs.Launch("ai-digest", validate.LaunchRunInput{Trigger: domain.TriggerManual}); err != nil {
		t.Fatalf("check off: %v", err)
	}
}

func TestApplyCredentialConnectsEveryNode(t *testing.T) {
	p := setupPreflight(t)
	cred, err := p.Credentials.Create("My GitHub", "github", map[string]any{"GITHUB_TOKEN": "ghp_x"})
	if err != nil {
		t.Fatal(err)
	}
	pf, err := p.Workflows.ApplyCredential("ai-digest", cred.ID)
	if err != nil {
		t.Fatal(err)
	}
	if accountByType(pf, "github") != nil || accountByType(pf, "pieces.slack") == nil {
		t.Fatalf("github should be connected, slack still missing: %+v", pf)
	}
	nodes, _ := p.Workflows.GraphNodes("ai-digest")
	for _, n := range nodes {
		want := n.Type == "github.listIssues" || n.Type == "github.getRepo"
		if got := n.Config[credentialIDKey] == string(cred.ID); got != want {
			t.Fatalf("node %s: credential applied=%v, want %v", n.Key, got, want)
		}
	}

	// Deleting it turns both nodes into "credential deleted".
	if err := p.Credentials.Delete(cred.ID); err != nil {
		t.Fatal(err)
	}
	pf, _ = p.Runs.Preflight("ai-digest")
	gh := accountByType(pf, "github")
	if gh == nil || gh.Nodes[0].Problem != ProblemCredentialDeleted {
		t.Fatalf("want credential_deleted, got %+v", gh)
	}
}

func TestPieceCredentialMissingRequiredField(t *testing.T) {
	p := setupPreflight(t)
	cred, err := p.Credentials.Create("Slack", "pieces.slack", map[string]any{"access_token": "xoxb"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Workflows.ApplyCredential("ai-digest", cred.ID); err != nil {
		t.Fatal(err)
	}
	// Clearing the token (allowed by Update) leaves the node incomplete.
	if _, err := p.Credentials.Update(cred.ID, nil, map[string]any{"access_token": ""}); err != nil {
		t.Fatal(err)
	}
	pf, _ := p.Runs.Preflight("ai-digest")
	slack := accountByType(pf, "pieces.slack")
	if slack == nil || slack.Nodes[0].Problem != ProblemIncomplete || slack.Nodes[0].MissingKeys[0] != "access_token" {
		t.Fatalf("want incomplete access_token, got %+v", slack)
	}
}
