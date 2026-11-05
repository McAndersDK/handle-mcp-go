package candidates_test

import (
	"encoding/json"
	"testing"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/candidates"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/match"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
)

func line(id, name string, total int64) candidates.Line {
	p := model.NewProduct("s", id, name, model.NewMoney(total, 0))
	note := ""
	return candidates.Line{Product: &p, Quantity: match.QtyOf(1), LineTotal: model.NewMoney(total, 0), Note: &note, Sure: true}
}

func TestOekoIsAlwaysAmongThem(t *testing.T) {
	others := []candidates.Line{line("1", "Mælk", 10), line("2", "Mælk", 11), line("3", "Mælk", 12),
		line("4", "Mælk", 13), line("5", "Mælk", 14), line("6", "Øko mælk", 30)}
	got := candidates.Candidates("mælk", line("0", "Mælk", 9), others)
	if len(got) != candidates.Limit || !got[0].Suggested || got[4].ID != "6" || got[3].ID != "3" {
		t.Errorf("%d candidates, last %s", len(got), got[len(got)-1].ID)
	}
	// an øko line that is picked already is not added twice (Python: == on the lines)
	others = []candidates.Line{line("1", "Øko mælk", 10), line("2", "Mælk", 11)}
	if got := candidates.Candidates("mælk", line("0", "Mælk", 9), others); len(got) != 3 {
		t.Errorf("%d candidates", len(got))
	}
	// a duplicate of a picked line (same fields) counts as picked
	dup := []candidates.Line{line("1", "Mælk", 10), line("1", "Mælk", 10), line("1", "Mælk", 10), line("1", "Mælk", 10), line("1", "Øko mælk", 10)}
	dup = append(dup[:4], line("9", "Øko mælk", 10))
	if got := candidates.Candidates("mælk", line("0", "Mælk", 9), dup); got[4].ID != "9" {
		t.Errorf("the øko line replaces the last: %s", got[4].ID)
	}
	if candidates.Candidates("mælk", candidates.Line{}, others) != nil {
		t.Error("no product, no candidates")
	}
}

func TestLeftOutFacts(t *testing.T) {
	b, _ := json.Marshal(candidates.Brief("mælk", line("1", "Mælk", 10), false))
	want := `{"id":"1","name":"Mælk","price":"10.00","quantity":1,"line_total":"10.00","note":"","name_match":"full"}`
	if string(b) != want {
		t.Errorf("%s", b)
	}
}
