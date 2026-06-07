package platform

import (
	"testing"

	"github.com/agently/api/internal/domain"
)

func TestPaginateFirstPage(t *testing.T) {
	items := []int{0, 1, 2, 3, 4}
	page := Paginate(items, domain.PageQuery{Limit: 2})
	if len(page.Items) != 2 || page.Items[0] != 0 || page.Items[1] != 1 {
		t.Fatalf("unexpected first page: %+v", page.Items)
	}
	if page.NextCursor == nil {
		t.Fatal("expected a next cursor")
	}
	if page.Total == nil || *page.Total != 5 {
		t.Fatalf("expected total 5, got %v", page.Total)
	}
}

func TestPaginateRoundTrip(t *testing.T) {
	items := []int{0, 1, 2, 3, 4}
	first := Paginate(items, domain.PageQuery{Limit: 2})
	second := Paginate(items, domain.PageQuery{Limit: 2, Cursor: *first.NextCursor})
	if len(second.Items) != 2 || second.Items[0] != 2 || second.Items[1] != 3 {
		t.Fatalf("unexpected second page: %+v", second.Items)
	}
	third := Paginate(items, domain.PageQuery{Limit: 2, Cursor: *second.NextCursor})
	if len(third.Items) != 1 || third.Items[0] != 4 {
		t.Fatalf("unexpected third page: %+v", third.Items)
	}
	if third.NextCursor != nil {
		t.Fatalf("expected no next cursor at end, got %v", *third.NextCursor)
	}
}

func TestPaginateEmptyEncodesArray(t *testing.T) {
	page := Paginate([]int{}, domain.PageQuery{Limit: 10})
	if page.Items == nil {
		t.Fatal("expected non-nil empty slice so JSON encodes []")
	}
}

func TestDecodeBadCursorResetsToZero(t *testing.T) {
	if got := decodeCursor("!!!not-base64!!!"); got != 0 {
		t.Fatalf("expected 0 for bad cursor, got %d", got)
	}
}
