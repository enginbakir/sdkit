package ui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeQuestions(t *testing.T) {
	norm := func(in string) (QuestionItem, error) {
		out, err := NormalizeQuestions([]byte(in))
		if err != nil {
			return QuestionItem{}, err
		}
		var v struct{ Questions []QuestionItem }
		if e := json.Unmarshal(out, &v); e != nil || len(v.Questions) != 1 {
			t.Fatalf("normalized output is not one question: %s (%v)", out, e)
		}
		return v.Questions[0], nil
	}

	q, err := norm(`{"questions":[{"question":"Hangisi?","options":[{"label":"A"},{"label":"B","description":"d"}],"multiSelect":true}]}`)
	if err != nil || q.Finding != nil || len(q.Options) != 2 || !q.MultiSelect {
		t.Errorf("a plain question must pass as it is: %+v %v", q, err)
	}

	q, _ = norm(`{"questions":[{"question":"Uygulansın mı?","finding":{"id":"F2","severity":"major","where":"spec.md:12","problem":"AC-4 ölçülemiyor","class":"fix","diff":"- hızlı\n+ 2 sn içinde"}}]}`)
	if f := q.Finding; f == nil || f.Severity != "MAJOR" || f.Class != "FIX" || f.ID != "F2" || !strings.Contains(f.Diff, "+ 2 sn") {
		t.Errorf("finding = %+v", f)
	}

	q, _ = norm(`{"questions":[{"question":"q","finding":{"problem":"p","severity":"catastrophic","class":"maybe"}}]}`)
	if f := q.Finding; f == nil || f.Severity != "" || f.Class != "" {
		t.Errorf("a severity or class the page does not know is dropped, the finding stays: %+v", f)
	}

	q, _ = norm(`{"questions":[{"question":"q","finding":{"id":"F1","severity":"MAJOR"}}]}`)
	if q.Finding != nil {
		t.Errorf("a finding with no problem to show is just the question: %+v", q.Finding)
	}

	q, _ = norm(`{"questions":[{"question":"q","finding":{"problem":"` + strings.Repeat("x", 5000) + `","diff":"` + strings.Repeat("y", 9000) + `"}}]}`)
	if n := len([]rune(q.Finding.Problem)); n > 1501 {
		t.Errorf("problem is %d runes, an essay does not belong on a card", n)
	}
	if n := len([]rune(q.Finding.Diff)); n > 4001 {
		t.Errorf("diff is %d runes", n)
	}

	var many []string
	for i := 0; i < 20; i++ {
		many = append(many, `{"label":"o"}`)
	}
	if q, _ = norm(`{"questions":[{"question":"q","options":[` + strings.Join(many, ",") + `]}]}`); len(q.Options) != 12 {
		t.Errorf("options = %d, want at most 12", len(q.Options))
	}

	for _, bad := range []string{`{}`, `{"questions":[]}`, `nope`, `{"questions":[{"question":"  "}]}`} {
		if _, err := NormalizeQuestions([]byte(bad)); err == nil {
			t.Errorf("%s must be refused", bad)
		}
	}
}
