package bot

import (
	"strings"
	"testing"
	"time"
)

func TestSearchPassages(t *testing.T) {
	now := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	catalog := Catalog{Documents: []Source{
		{Country: "Испания", Checked: "2026-11-21", Title: "Семья", Texts: []SourceText{
			{File: "texts/family.md", URL: "https://example.org/family.pdf", Title: "Семья", chunks: []string{"certificado de matrimonio"}},
		}},
		{Country: "Испания", Checked: "2026-10-01", Summary: "obsolete fact", Texts: []SourceText{{chunks: []string{"obsolete text"}}}},
		{Country: "Грузия", Checked: "2026-11-21", Texts: []SourceText{{chunks: []string{"wrong jurisdiction"}}}},
	}}
	plan := SearchPlan{Files: []string{"texts/family.md"}, Terms: []string{"matrimonio"}}
	for _,question := range []string{"Какие документы в Испании?", "А для супруга?"} {
		result := catalog.Search("Грузия",question,now,plan)
		if !strings.Contains(result,"certificado de matrimonio") || !strings.Contains(result,"https://example.org/family.pdf") || !strings.Contains(result,"2026-11-21") {
			t.Fatal("selected original passage or its citation missing")
		}
		if strings.Contains(result,"obsolete") || (mentions(question,"Испания") && strings.Contains(result,"wrong jurisdiction")) {
			t.Fatal("included stale or unrelated evidence")
		}
	}
}
