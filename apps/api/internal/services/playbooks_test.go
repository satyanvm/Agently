package services

import (
	"strings"
	"testing"

	"github.com/agently/api/internal/domain"
)

// Every shipped template must pass the planner's own checks: structure,
// references, schedule, and "nothing publishes without approval".
func TestPlaybookTemplatesValidate(t *testing.T) {
	all, err := loadPlaybooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 playbooks, got %d", len(all))
	}
	cat := LoadCatalog()
	for _, pb := range all {
		t.Run(pb.ID, func(t *testing.T) {
			keys := map[string]bool{}
			for k := range pb.DefaultInput {
				keys[k] = true
			}
			for _, in := range pb.Inputs {
				if _, ok := pb.DefaultInput[in.Key]; !ok {
					t.Errorf("input %q has no defaultInput entry", in.Key)
				}
			}
			if errs := validateGraph(pb.Nodes, cat); len(errs) > 0 {
				t.Errorf("validateGraph: %s", strings.Join(errs, "\n"))
			}
			if errs := validateRefs(pb.Nodes, cat, keys); len(errs) > 0 {
				t.Errorf("validateRefs: %s", strings.Join(errs, "\n"))
			}
			if err := ValidateSchedule(pb.Schedule); err != nil {
				t.Errorf("schedule: %v", err)
			}
			// Every integration step (anything that writes outside Agently
			// except internal Slack alerts) sits behind an approval.
			byKey := map[string]domain.GraphNode{}
			for _, n := range pb.Nodes {
				byKey[n.Key] = n
			}
			var above func(string, map[string]bool) bool
			above = func(k string, seen map[string]bool) bool {
				for _, d := range byKey[k].DependsOn {
					if seen[d] {
						continue
					}
					seen[d] = true
					if byKey[d].Type == approvalType || above(d, seen) {
						return true
					}
				}
				return false
			}
			for _, n := range pb.Nodes {
				if strings.HasPrefix(n.Type, "pieces.") && !above(n.Key, map[string]bool{}) {
					t.Errorf("%s (%s) has no approval before it", n.Key, n.Type)
				}
			}
		})
	}
}

func TestInstallPlaybook(t *testing.T) {
	p := NewPlatform(Options{})
	list, err := p.Playbooks.List()
	if err != nil || len(list) != 3 {
		t.Fatalf("list = %v, %v", list, err)
	}
	var pricing PlaybookSummary
	for _, s := range list {
		if s.ID == "competitor-pricing-change" {
			pricing = s
		}
	}
	if pricing.Approvals != 4 || len(pricing.Logins) != 2 || pricing.Logins[0].Connected {
		t.Fatalf("summary = %+v (4 approvals; Google Docs + Gmail logins, not connected)", pricing)
	}

	if _, err := p.Playbooks.Install("competitor-pricing-change", "", map[string]any{"approver": "Rahul"}); err == nil ||
		!strings.Contains(err.Error(), "Battlecard Google Doc ID") {
		t.Fatalf("missing inputs should be named, got %v", err)
	}

	// With one stored Google Docs login, it is attached to the Docs steps.
	cred := p.Repos.Credentials.Insert(domain.Credential{ID: domain.NewCredentialId(), WorkspaceID: p.WorkspaceID,
		Type: "pieces.google-docs", Name: "Docs", Data: map[string]any{"access_token": "x"}})
	sum, err := p.Playbooks.Install("competitor-pricing-change", "Pricing watch", map[string]any{
		"approver": "Rahul", "battlecard_doc_id": "doc123", "sales_slack_webhook": "https://hooks.slack.test/x",
		"at_risk_customer_emails": "a@x.test, b@x.test", "email_subject": "Pricing",
	})
	if err != nil {
		t.Fatal(err)
	}
	wf, _ := p.Repos.Workflows.GetBySlug(sum.Slug)
	if wf.Trigger != domain.TriggerSchedule || wf.Schedule == nil || *wf.Schedule != "weekdays 08:00" {
		t.Fatalf("installed trigger=%s schedule=%v", wf.Trigger, wf.Schedule)
	}
	if got := wf.DefaultInput["at_risk_customer_emails"]; len(got.([]string)) != 2 {
		t.Fatalf("list input = %#v", got)
	}
	nodes, _ := p.Workflows.GraphNodes(sum.Slug)
	byKey := map[string]domain.GraphNode{}
	for _, n := range nodes {
		byKey[n.Key] = n
	}
	if byKey["update_battlecard"].Config["__credentialId"] != string(cred.ID) {
		t.Fatalf("docs login not attached: %#v", byKey["update_battlecard"].Config)
	}
	if _, ok := byKey["email_draft"].Config["__credentialId"]; ok {
		t.Fatal("no Gmail login stored: nothing should be attached")
	}
	if byKey["start"].Config["cron"] != "weekdays 08:00" {
		t.Fatalf("trigger cron = %#v", byKey["start"].Config)
	}
	if _, err := p.Playbooks.Install("nope", "", nil); err == nil {
		t.Fatal("unknown playbook should 404")
	}
}
