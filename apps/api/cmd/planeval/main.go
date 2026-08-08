package main

// planeval measures piece-selection RECALL: for a golden set of realistic
// prompts, does each selection strategy surface the pieces a correct workflow
// needs? Strategies compared (services.PieceSelectionMethods):
//
//   lexical    — the retired term-overlap baseline, retained only for comparison
//   embeddings — the offline sidecar prefilter alone (needs embeddings.json/.bin
//                next to the pieces index + VOYAGE_API_KEY)
//   router     — the primary path: small-model call over the piece directory
//                (needs an LLM key; includes the embedding prefilter when present)
//
// Expectations are ANY-OF groups: "text me" is satisfied by twilio OR clicksend
// OR any other SMS piece — the graph only needs one of them. Recall@N =
// satisfied groups / total groups. Run from apps/api:
//
//   go run ./cmd/planeval          # summary
//   go run ./cmd/planeval -v      # plus every miss
//
// This is an eval, not a test: it costs LLM/embedding calls and its numbers
// move with models. Use it to compare strategies, not as CI.

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/agently/api/internal/services"
)

type golden struct {
	prompt string
	groups [][]string // each group is satisfied by ANY of its slugs
}

var goldens = []golden{
	{"when a customer pays in stripe, add them to my mailchimp audience and text me",
		[][]string{{"stripe"}, {"mailchimp"}, {"twilio", "clicksend", "messagebird", "seven", "smsmode"}}},
	{"add a row to my spreadsheet whenever a new github issue is opened",
		[][]string{{"google-sheets", "microsoft-excel-365", "smartsheet"}, {"github"}}},
	{"post to my team channel when the daily report is ready",
		[][]string{{"slack", "microsoft-teams", "discord", "googlechat", "mattermost"}}},
	{"generate an image from today's top headline and post it on twitter",
		[][]string{{"openai", "stability-ai", "runware", "image-router", "modelslab"}, {"twitter"}}},
	{"transcribe my zoom recordings and save the summaries to notion",
		[][]string{{"zoom"}, {"notion"}}},
	{"create a trello card for each new typeform response",
		[][]string{{"trello"}, {"typeform"}}},
	{"update the CRM when a deal closes",
		[][]string{{"hubspot", "salesforce", "pipedrive", "zoho-crm", "freshsales"}}},
	{"send an sms reminder 10 minutes before each calendar event",
		[][]string{{"google-calendar", "microsoft-outlook-calendar"}, {"twilio", "clicksend", "messagebird", "seven", "smsmode"}}},
	{"back up new email attachments to cloud storage",
		[][]string{{"gmail", "microsoft-outlook", "imap"}, {"google-drive", "dropbox", "amazon-s3", "box", "microsoft-onedrive"}}},
	{"when someone books a meeting with me, create an invoice for them",
		[][]string{{"calendly", "cal-com", "savvycal", "tidycal", "acuity-scheduling"}, {"quickbooks", "xero", "invoiceninja", "zoho-invoice"}}},
	{"notify me on my phone when the website goes down",
		[][]string{{"uptimerobot"}, {"pushover", "pushbullet", "ntfy", "gotify", "twilio"}}},
	{"scrape competitor pricing pages weekly and log changes to a database",
		[][]string{{"firecrawl", "webscraping-ai", "browse-ai", "scrapeless", "apify", "browserless"}, {"postgres", "mysql", "mongodb", "supabase", "airtable"}}},
	{"translate new zendesk tickets to english",
		[][]string{{"zendesk"}, {"deepl"}}},
	{"add new shopify orders to a google sheet and send a whatsapp confirmation",
		[][]string{{"shopify"}, {"google-sheets"}, {"whatsapp"}}},
	{"when a new lead fills out the form, enrich their email and add them to my outreach campaign",
		[][]string{{"typeform", "jotform", "tally", "opnform"}, {"hunter", "apollo", "lusha", "proxycurl", "enrichlayer"}, {"lemlist", "klaviyo", "smartlead", "instantly-ai", "mailchimp"}}},
	{"summarize each new video on my youtube channel and tweet the summary",
		[][]string{{"youtube"}, {"twitter"}}},
	{"watch an rss feed and email me new articles",
		[][]string{{"rss"}}},
	{"sync new airtable records to my postgres database",
		[][]string{{"airtable"}, {"postgres"}}},
	{"charge customers a monthly subscription and record the payment in quickbooks",
		[][]string{{"stripe", "razorpay", "square", "paddle", "recurly", "chargebee"}, {"quickbooks"}}},
	{"text me the weather every morning",
		[][]string{{"twilio", "clicksend", "messagebird", "seven", "smsmode"}}},
	{"when I get a new email from my boss, create a task in my todo app",
		[][]string{{"gmail", "microsoft-outlook", "imap"}, {"todoist", "ticktick", "microsoft-todo", "google-tasks", "clickup", "asana"}}},
	{"record form submissions in a spreadsheet and let the team know on slack",
		[][]string{{"google-sheets", "microsoft-excel-365"}, {"slack"}}},
}

func main() {
	max := flag.Int("max", 12, "selection budget (mirrors maxClusterCalls)")
	verbose := flag.Bool("v", false, "print every missed group")
	pause := flag.Duration("pause", 0, "wait between prompts (rate-limit hygiene, e.g. -pause 10s)")
	retries := flag.Int("retries", 0, "re-attempt a prompt when the router got rate-limited (waits -pause between tries)")
	flag.Parse()
	ctx := context.Background()

	type tally struct{ hit, total, prompts int }
	stats := map[string]*tally{}
	misses := map[string][]string{}
	failures := map[string][]string{}

	for i, g := range goldens {
		if i > 0 && *pause > 0 {
			time.Sleep(*pause)
		}
		methods, errs := services.PieceSelectionMethods(ctx, g.prompt, *max)
		for r := 0; r < *retries; r++ {
			if _, ok := methods["router"]; ok {
				break
			}
			time.Sleep(*pause)
			again, againErrs := services.PieceSelectionMethods(ctx, g.prompt, *max)
			if sel, ok := again["router"]; ok {
				methods["router"] = sel
				delete(errs, "router")
			} else {
				errs["router"] = againErrs["router"]
			}
		}
		// A strategy that errored is NOT zero recall — it is an unanswered prompt.
		// Record why so the summary can say so instead of quietly scoring it 0.
		for name, err := range errs {
			failures[name] = append(failures[name], fmt.Sprintf("%q: %v", g.prompt, err))
		}
		for name, slugs := range methods {
			t := stats[name]
			if t == nil {
				t = &tally{}
				stats[name] = t
			}
			t.prompts++
			selected := map[string]bool{}
			for _, s := range slugs {
				selected[s] = true
			}
			for _, group := range g.groups {
				t.total++
				sat := false
				for _, want := range group {
					if selected[want] {
						sat = true
						break
					}
				}
				if sat {
					t.hit++
				} else {
					misses[name] = append(misses[name],
						fmt.Sprintf("%q wanted one of [%s], got [%s]", g.prompt, strings.Join(group, " "), strings.Join(slugs, " ")))
				}
			}
		}
		fmt.Print(".")
	}
	fmt.Println()

	names := make([]string, 0, len(stats))
	for n := range stats {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Printf("\nrecall@%d over %d prompts:\n", *max, len(goldens))
	for _, n := range names {
		t := stats[n]
		note := ""
		if t.prompts < len(goldens) {
			note = fmt.Sprintf("  [only %d/%d prompts answered — treat with suspicion]", t.prompts, len(goldens))
		}
		fmt.Printf("  %-11s %3d/%3d groups (%.0f%%)%s\n", n, t.hit, t.total, 100*float64(t.hit)/float64(t.total), note)
	}
	for _, n := range []string{"embeddings", "router"} {
		if _, ok := stats[n]; !ok {
			fmt.Printf("  %-11s (never answered — needs %s)\n", n,
				map[string]string{
					"embeddings": "the embeddings sidecar + VOYAGE_API_KEY",
					"router":     "ANTHROPIC_API_KEY (plus the sidecar it prefilters through)",
				}[n])
		}
	}
	// Errors are reported separately from misses: a strategy that failed on half
	// the prompts and scored well on the rest is not an 80% strategy.
	if len(failures) > 0 {
		fmt.Println("\nfailures (these prompts were NOT scored):")
		fnames := make([]string, 0, len(failures))
		for n := range failures {
			fnames = append(fnames, n)
		}
		sort.Strings(fnames)
		for _, n := range fnames {
			fmt.Printf("  %-11s %d/%d prompts failed\n", n, len(failures[n]), len(goldens))
			for _, f := range failures[n] {
				fmt.Printf("      %s\n", f)
			}
		}
	}
	if *verbose {
		for _, n := range names {
			if len(misses[n]) == 0 {
				continue
			}
			fmt.Printf("\n%s misses:\n", n)
			for _, m := range misses[n] {
				fmt.Println("  - " + m)
			}
		}
	}
}
