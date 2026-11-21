package bot

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type Source struct {
	Country string       `json:"country"`
	Title   string       `json:"title"`
	URL     string       `json:"url"`
	Checked string       `json:"checked_at"`
	Summary string       `json:"summary"`
	Texts   []SourceText `json:"texts,omitempty"`
	Topics  []string     `json:"topics"`
}

type Catalog struct {
	Documents []Source `json:"documents"`
	Digest    string   `json:"-"`
}

func LoadCatalog(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Catalog
	if err = json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("knowledge file %s: %w", path, err)
	}
	if len(c.Documents) == 0 {
		return nil, fmt.Errorf("knowledge file %s is empty", path)
	}
	seen := map[string]bool{}
	today := time.Now().UTC().Format(time.DateOnly)
	for i, d := range c.Documents {
		u, e := url.Parse(d.URL)
		_, dateErr := time.Parse(time.DateOnly, d.Checked)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || seen[d.URL] || dateErr != nil || d.Checked > today || strings.TrimSpace(d.Country) == "" || strings.TrimSpace(d.Title) == "" || strings.TrimSpace(d.Summary) == "" || len([]rune(d.Summary)) > 1500 {
			return nil, fmt.Errorf("invalid or duplicate knowledge document %d in %s", i+1, path)
		}
		seen[d.URL] = true
		for j := range c.Documents[i].Texts {
			if err := c.Documents[i].Texts[j].load(filepath.Dir(path)); err != nil {
				return nil, err
			}
		}
	}
	c.Digest = fmt.Sprintf("%x", sha256.Sum256(data))
	return &c, nil
}

var countryWords = map[string][]string{
	"Грузия": {"грузи", "georgia", "ge"}, "Армения": {"армени", "armenia", "am"},
	"Турция": {"турци", "turkey", "türkiye", "tr"}, "Казахстан": {"казахстан", "kazakhstan", "kz"},
	"Сербия": {"серби", "serbia", "rs"}, "Испания": {"испани", "spain", "es"},
	"Германия": {"германи", "germany", "de"}, "Таиланд": {"таиланд", "тайланд", "thailand", "th"},
	"Вьетнам": {"вьетнам", "vietnam", "vn"},
}

func mentions(text, country string) bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
	for _, word := range words {
		if word == strings.ToLower(country) {
			return true
		}
		for _, alias := range countryWords[country] {
			if word == alias || (len([]rune(alias)) > 2 && strings.HasPrefix(word, alias)) {
				return true
			}
		}
	}
	return false
}

func (c *Catalog) Context(profileCountries, question string, now time.Time) string {
	return c.Search(profileCountries, question, now, SearchPlan{})
}
