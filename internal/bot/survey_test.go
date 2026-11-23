package bot

import (
	"strings"
	"testing"
)

func TestSurveyCompletesAndBranches(t *testing.T) {
	p := map[string]string{}
	key := SurveyNext(p)
	count := 0
	for key != "" {
		count++
		if count > 40 {
			t.Fatal("survey loop")
		}
		key, _ = SurveyAnswer(p, key, "Пропустить")
	}
	if count != 28 {
		t.Fatalf("base questions=%d", count)
	}
	p["family"] = "С детьми"
	delete(p, "family_details")
	if SurveyNext(p) != "family_details" {
		t.Fatal("missing family detail")
	}
	key, _ = SurveyAnswer(p, "family_details", "Двое взрослых, ребёнок 7 лет")
	if key != "" {
		t.Fatal(key)
	}
	key, _ = SurveyAnswer(p, "ties", "Да")
	if key != "ties_details" {
		t.Fatal(key)
	}
}
func TestSurveyMultiAndCountry(t *testing.T) {
	p := map[string]string{}
	key, hint := SurveyAnswer(p, "current_country", "Другая страна")
	if key != "current_country" || hint == "" || p[key] != "" {
		t.Fatal("saved placeholder")
	}
	SurveyAnswer(p, "priorities", "Климат")
	SurveyAnswer(p, "priorities", "Работа")
	if !strings.Contains(p["priorities"], "Климат") {
		t.Fatal("lost selection")
	}
	SurveyAnswer(p, "priorities", "Климат")
	if p["priorities"] != "Работа" {
		t.Fatal("toggle failed")
	}
	key, _ = SurveyAnswer(p, "priorities", "Готово")
	if key == "priorities" {
		t.Fatal("stuck")
	}
	if SurveyEditKey("0") != "" || SurveyEditKey("1") != "current_country" {
		t.Fatal("edit mapping")
	}
}
