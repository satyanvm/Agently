package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/services"
)

func intelServer(t *testing.T) (*services.Platform, http.Handler) {
	t.Helper()
	p := services.NewPlatform(services.Options{})
	return p, NewRouter(p)
}

func call(t *testing.T, h http.Handler, method, path string, body any) (*httptest.ResponseRecorder, any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func dataOf(body any) map[string]any {
	d, _ := body.(map[string]any)
	return d
}

func listOf(body any) []any {
	l, _ := body.([]any)
	return l
}

func TestCompetitorsAPI(t *testing.T) {
	p, h := intelServer(t)

	rec, env := call(t, h, http.MethodPost, "/api/competitors", map[string]any{
		"name": "Acme", "website": "https://acme.test",
		"pages": []any{
			map[string]any{"kind": "pricing", "url": "https://acme.test/pricing"},
			map[string]any{"kind": "changelog", "url": "https://acme.test/changelog", "render": "browser"},
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	created := dataOf(env)
	id, _ := created["id"].(string)
	if !strings.HasPrefix(id, "cmp_") {
		t.Fatalf("id = %q", id)
	}
	pages, _ := created["pages"].([]any)
	if first, _ := pages[0].(map[string]any); len(pages) != 2 || first["render"] != "http" {
		t.Fatalf("pages = %#v (render should default to http)", pages)
	}

	if rec, _ := call(t, h, http.MethodPost, "/api/competitors", map[string]any{"name": " acme "}); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate name = %d, want 409", rec.Code)
	}

	bad := []map[string]any{
		{},
		{"name": "X", "pages": []any{map[string]any{"kind": "pricing", "url": "ftp://x"}}},
		{"name": "X", "pages": []any{map[string]any{"kind": "homepage", "url": "https://x.test"}}},
		{"name": "X", "pages": []any{map[string]any{"url": "https://x.test"}}},
		{"name": "X", "pages": []any{map[string]any{"kind": "blog", "url": "https://x.test", "render": "curl"}}},
		{"name": "X", "pages": []any{
			map[string]any{"kind": "blog", "url": "https://x.test"},
			map[string]any{"kind": "pricing", "url": "https://x.test"},
		}},
		{"name": "X", "website": "acme"},
	}
	for i, body := range bad {
		if rec, _ := call(t, h, http.MethodPost, "/api/competitors", body); rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
			t.Fatalf("bad body %d = %d %s, want a validation error", i, rec.Code, rec.Body.String())
		}
	}

	rec, env = call(t, h, http.MethodPut, "/api/competitors/"+id, map[string]any{"notes": "main rival"})
	if rec.Code != http.StatusOK || dataOf(env)["notes"] != "main rival" || dataOf(env)["name"] != "Acme" {
		t.Fatalf("update = %d %s", rec.Code, rec.Body.String())
	}

	rec, _ = call(t, h, http.MethodDelete, "/api/competitors/"+id, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("archive = %d", rec.Code)
	}
	_, env = call(t, h, http.MethodGet, "/api/competitors", nil)
	if list := listOf(env); len(list) != 0 {
		t.Fatalf("archived competitor still listed: %#v", list)
	}
	_, env = call(t, h, http.MethodGet, "/api/competitors?includeArchived=true", nil)
	if list := listOf(env); len(list) != 1 {
		t.Fatalf("includeArchived list = %#v", list)
	}
	rec, env = call(t, h, http.MethodPut, "/api/competitors/"+id, map[string]any{"archived": false})
	if rec.Code != http.StatusOK || dataOf(env)["archivedAt"] != nil {
		t.Fatalf("restore = %d %s", rec.Code, rec.Body.String())
	}
	if rec, _ := call(t, h, http.MethodGet, "/api/competitors/cmp_missing", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing competitor = %d", rec.Code)
	}

	// Snapshots and signals are written by the reasoner; seed them directly.
	ws := p.WorkspaceID
	cid := domain.CompetitorId(id)
	snap := p.Repos.Snapshots.Insert(domain.PageSnapshot{
		ID: domain.NewPageSnapshotId(), WorkspaceID: ws, CompetitorID: &cid, PageKind: "pricing",
		URL: "https://acme.test/pricing", FetchedVia: "http", ContentHash: "h", Content: "Starter $12",
		FetchedAt: "2026-10-18T09:00:00.000Z", LastSeenAt: "2026-10-18T09:00:00.000Z",
	})
	_, env = call(t, h, http.MethodGet, "/api/competitors/"+id+"/snapshots", nil)
	list := listOf(env)
	if len(list) != 1 {
		t.Fatalf("snapshots = %#v", env)
	}
	if row, _ := list[0].(map[string]any); row["content"] != nil || row["chars"] != float64(11) {
		t.Fatalf("snapshot summary should omit content and count chars: %#v", row)
	}
	_, env = call(t, h, http.MethodGet, "/api/snapshots/"+string(snap.ID), nil)
	if dataOf(env)["content"] != "Starter $12" {
		t.Fatalf("snapshot detail = %#v", env)
	}

	p.Repos.Signals.Insert(domain.Signal{
		ID: domain.NewSignalId(), WorkspaceID: ws, Source: "page_change", CompetitorID: &cid,
		Title: "Acme pricing page changed", DetectedAt: "2026-10-18T09:00:00.000Z",
	})
	p.Repos.Signals.Insert(domain.Signal{
		ID: domain.NewSignalId(), WorkspaceID: ws, Source: "news", Title: "Acme raises", DetectedAt: "2026-10-18T10:00:00.000Z",
	})
	_, env = call(t, h, http.MethodGet, "/api/signals?source=page_change", nil)
	if list := listOf(env); len(list) != 1 {
		t.Fatalf("signals by source = %#v", env)
	}
	_, env = call(t, h, http.MethodGet, "/api/signals?since=2026-10-18T09:30:00Z", nil)
	if list := listOf(env); len(list) != 1 {
		t.Fatalf("signals since = %#v", env)
	}
	if rec, _ := call(t, h, http.MethodGet, "/api/signals?source=tweet", nil); rec.Code == http.StatusOK {
		t.Fatal("unknown source should be rejected")
	}
	if rec, _ := call(t, h, http.MethodGet, "/api/signals?since=yesterday", nil); rec.Code == http.StatusOK {
		t.Fatal("bad since should be rejected")
	}
}
