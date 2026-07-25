package services

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixture with the shapes the router/prefilter care about: display names,
// categories, a trigger, Custom API Call boilerplate, and multiple clusters.
const routerFixture = `{
  "version": 1,
  "nodes": [
    {
      "id": "pieces.google-sheets.add_row",
      "piece": "@activepieces/piece-google-sheets",
      "pieceVersion": "1.0.0",
      "pieceDisplayName": "Google Sheets",
      "categories": ["PRODUCTIVITY"],
      "action": "add_row",
      "label": "Add Row",
      "description": "Appends a row of values to a worksheet.",
      "kind": "action",
      "search": ["sheets", "row"],
      "auth": {"type": "oauth2", "credentialKey": "AP_GOOGLE_SHEETS_AUTH", "required": true},
      "props": []
    },
    {
      "id": "pieces.google-sheets.custom_api_call",
      "piece": "@activepieces/piece-google-sheets",
      "pieceVersion": "1.0.0",
      "pieceDisplayName": "Google Sheets",
      "categories": ["PRODUCTIVITY"],
      "action": "custom_api_call",
      "label": "Custom API Call",
      "description": "Call any Google Sheets endpoint.",
      "kind": "action",
      "search": ["custom"],
      "auth": {"type": "oauth2", "credentialKey": "AP_GOOGLE_SHEETS_AUTH", "required": true},
      "props": []
    },
    {
      "id": "pieces.google-sheets.new_row",
      "piece": "@activepieces/piece-google-sheets",
      "pieceVersion": "1.0.0",
      "pieceDisplayName": "Google Sheets",
      "categories": ["PRODUCTIVITY"],
      "action": "new_row",
      "label": "New Row Added",
      "description": "Fires when a row is appended.",
      "kind": "trigger",
      "strategy": "polling",
      "search": ["sheets", "new"],
      "auth": {"type": "oauth2", "credentialKey": "AP_GOOGLE_SHEETS_AUTH", "required": true},
      "props": []
    },
    {
      "id": "pieces.twilio.send_sms",
      "piece": "@activepieces/piece-twilio",
      "pieceVersion": "1.0.0",
      "pieceDisplayName": "Twilio",
      "categories": ["COMMUNICATION"],
      "action": "send_sms",
      "label": "Send SMS",
      "description": "Sends a text message.",
      "kind": "action",
      "search": ["sms", "text"],
      "auth": {"type": "basic_auth", "credentialKey": "AP_TWILIO_AUTH", "required": true},
      "props": []
    }
  ]
}`

func routerCatalog(t *testing.T) *Catalog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.json")
	if err := os.WriteFile(path, []byte(routerFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	cat := loadCatalogFrom(catalogDir())
	mergePiecesIndex(cat, path)
	return cat
}

func TestMergePiecesIndex_DisplayNameAndCategories(t *testing.T) {
	cat := routerCatalog(t)
	f := cat.Clusters["pieces.google-sheets"]
	if f.Label != "Google Sheets (Activepieces)" {
		t.Fatalf("label = %q, want index displayName", f.Label)
	}
	if len(f.Categories) != 1 || f.Categories[0] != "PRODUCTIVITY" {
		t.Fatalf("categories = %v", f.Categories)
	}
}

func TestPieceDirectory_ListsEverythingUntruncated(t *testing.T) {
	cat := routerCatalog(t)
	dir := pieceDirectory(cat, []string{"pieces.google-sheets", "pieces.twilio"})

	for _, want := range []string{
		"google-sheets — Google Sheets [PRODUCTIVITY]:",
		"Add Row",
		"triggers: New Row Added",
		"twilio — Twilio [COMMUNICATION]: Send SMS",
	} {
		if !strings.Contains(dir, want) {
			t.Fatalf("directory missing %q:\n%s", want, dir)
		}
	}
	// Boilerplate is stated once in the header, not per piece.
	if n := strings.Count(dir, "Custom API Call"); n != 1 {
		t.Fatalf("Custom API Call appears %d times, want 1 (header only):\n%s", n, dir)
	}
}

func TestRoutePieceClusters_NoKeyFallsBack(t *testing.T) {
	for _, k := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENAI_API_KEY"} {
		t.Setenv(k, "")
	}
	cat := routerCatalog(t)
	if _, ok := routePieceClusters(context.Background(), cat, "text me", []string{"pieces.twilio"}, 12); ok {
		t.Fatal("router must report failure with no LLM key so the lexical fallback runs")
	}
	// No piece clusters at all is a valid empty answer, not a failure.
	if sel, ok := routePieceClusters(context.Background(), cat, "text me", nil, 12); !ok || len(sel) != 0 {
		t.Fatalf("empty pieceKeys: sel=%v ok=%v, want empty+true", sel, ok)
	}
}

func TestCapFairly_NoAlphabeticalBias(t *testing.T) {
	// The old sort+truncate would emit 8×aardvark and drop zebra entirely.
	byCluster := map[string][]string{
		"pieces.aardvark": {"pieces.aardvark.a1", "pieces.aardvark.a2", "pieces.aardvark.a3", "pieces.aardvark.a4", "pieces.aardvark.a5", "pieces.aardvark.a6", "pieces.aardvark.a7", "pieces.aardvark.a8"},
		"pieces.zebra":    {"pieces.zebra.z1", "pieces.zebra.z2", "pieces.zebra.z3", "pieces.zebra.z4", "pieces.zebra.z5", "pieces.zebra.z6", "pieces.zebra.z7", "pieces.zebra.z8"},
	}
	got := capFairly(byCluster, 8)
	if len(got) != 8 {
		t.Fatalf("len = %d, want 8", len(got))
	}
	var aard, zebra int
	for _, id := range got {
		if strings.HasPrefix(id, "pieces.aardvark.") {
			aard++
		}
		if strings.HasPrefix(id, "pieces.zebra.") {
			zebra++
		}
	}
	if aard != 4 || zebra != 4 {
		t.Fatalf("split = %d/%d, want 4/4: %v", aard, zebra, got)
	}
	// Each cluster's contribution must be its TOP ids (model ranking), not arbitrary.
	for _, want := range []string{"pieces.zebra.z1", "pieces.zebra.z4", "pieces.aardvark.a1", "pieces.aardvark.a4"} {
		found := false
		for _, id := range got {
			if id == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing top-ranked id %s in %v", want, got)
		}
	}

	// Under the cap: everything passes through.
	small := map[string][]string{"c": {"c.1", "c.2"}}
	if got := capFairly(small, 32); len(got) != 2 {
		t.Fatalf("under cap: %v", got)
	}
	if got := capFairly(map[string][]string{}, 32); len(got) != 0 {
		t.Fatalf("empty: %v", got)
	}
}

/* ---------------------------- embedding prefilter ---------------------------- */

// writeEmbeddingsFixture writes a manifest+bin sidecar with the given
// id→vector rows (vectors are stored as-is; tests use pre-normalized values).
func writeEmbeddingsFixture(t *testing.T, dir string, dims int, rows []struct {
	ID  string
	Vec []float32
}) string {
	t.Helper()
	ids := make([]string, len(rows))
	bin := make([]byte, len(rows)*dims*4)
	for i, r := range rows {
		ids[i] = r.ID
		for j, v := range r.Vec {
			binary.LittleEndian.PutUint32(bin[(i*dims+j)*4:], math.Float32bits(v))
		}
	}
	manifest := `{"version":1,"model":"gemini-embedding-001","dims":` + itoa(dims) + `,"ids":["` + strings.Join(ids, `","`) + `"]}`
	mPath := filepath.Join(dir, "embeddings.json")
	if err := os.WriteFile(mPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "embeddings.bin"), bin, 0o644); err != nil {
		t.Fatal(err)
	}
	return mPath
}

func TestLoadPieceEmbeddingsFrom(t *testing.T) {
	dir := t.TempDir()
	mPath := writeEmbeddingsFixture(t, dir, 2, []struct {
		ID  string
		Vec []float32
	}{
		{"pieces.twilio.send_sms", []float32{1, 0}},
		{"pieces.google-sheets.add_row", []float32{0, 1}},
	})

	emb := loadPieceEmbeddingsFrom(mPath)
	if emb == nil || emb.Dims != 2 || len(emb.IDs) != 2 {
		t.Fatalf("loaded = %+v", emb)
	}
	if emb.Vecs[0] != 1 || emb.Vecs[3] != 1 {
		t.Fatalf("vecs = %v", emb.Vecs)
	}

	// Truncated bin (size mismatch) must load as nil, never panic.
	if err := os.Truncate(filepath.Join(dir, "embeddings.bin"), 4); err != nil {
		t.Fatal(err)
	}
	if loadPieceEmbeddingsFrom(mPath) != nil {
		t.Fatal("size-mismatched bin must be rejected")
	}
	if loadPieceEmbeddingsFrom(filepath.Join(dir, "absent.json")) != nil {
		t.Fatal("missing manifest must be nil")
	}
	if loadPieceEmbeddingsFrom("") != nil {
		t.Fatal("empty path must be nil")
	}
}

func TestRankPieceClusters_MaxRollupAndOrder(t *testing.T) {
	cat := routerCatalog(t)
	dir := t.TempDir()
	// google-sheets has TWO vectors; only its best one may count (max, not sum):
	// with sum, 0.6+0.6 would beat twilio's 0.9.
	mPath := writeEmbeddingsFixture(t, dir, 2, []struct {
		ID  string
		Vec []float32
	}{
		{"pieces.twilio.send_sms", []float32{0.9, float32(math.Sqrt(1 - 0.81))}},
		{"pieces.google-sheets.add_row", []float32{0.6, 0.8}},
		{"pieces.google-sheets.new_row", []float32{0.6, 0.8}},
		{"pieces.unknown.ghost", []float32{1, 0}}, // not in catalog → ignored
	})
	emb := loadPieceEmbeddingsFrom(mPath)
	if emb == nil {
		t.Fatal("fixture failed to load")
	}

	query := []float32{1, 0}
	keys := []string{"pieces.twilio", "pieces.google-sheets"}
	got := rankPieceClusters(cat, emb, query, keys, 10)
	if len(got) != 2 || got[0] != "pieces.twilio" || got[1] != "pieces.google-sheets" {
		t.Fatalf("ranked = %v, want twilio first (max roll-up)", got)
	}

	// topN truncation and the allowed-set filter.
	if got := rankPieceClusters(cat, emb, query, keys, 1); len(got) != 1 || got[0] != "pieces.twilio" {
		t.Fatalf("topN=1: %v", got)
	}
	if got := rankPieceClusters(cat, emb, query, []string{"pieces.google-sheets"}, 10); len(got) != 1 || got[0] != "pieces.google-sheets" {
		t.Fatalf("allowed filter: %v", got)
	}
}

func TestPrefilterPieceClusters_UnavailableIsNotFatal(t *testing.T) {
	t.Setenv("PIECES_EMBEDDINGS_PATH", filepath.Join(t.TempDir(), "absent.json"))
	// The process-wide cache may already hold a real sidecar from another test;
	// bypass it by checking the loader directly (prefilter shares its logic).
	if loadPieceEmbeddingsFrom(piecesEmbeddingsPath()) != nil {
		t.Fatal("absent sidecar must be nil → prefilter reports unavailable")
	}
}
