package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"migr-ai/internal/bot"
)

const maxBodyBytes = 8 << 20

var (
	scriptStyle = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)\s*>`)
	tags        = regexp.MustCompile(`<[^>]+>`)
	whitespace  = regexp.MustCompile(`\s+`)
	titleTag    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title\s*>`)
	anchorTag   = regexp.MustCompile(`(?is)<a\b[^>]*href\s*=\s*["']?([^"' >]+)["']?[^>]*>(.*?)</a\s*>`)
)

type job struct {
	ID          int64
	CountryCode string
	Country     string
	Topic       string
	Limit       int
	RequestedBy int64
}

type seed struct {
	ID        int64
	URL       string
	Title     string
	Authority string
	Language  string
}

type candidate struct {
	URL       string
	Title     string
	Authority string
	Language  string
	Seed      bool
}

type anchor struct{ URL, Text string }

type fetchedPage struct {
	Status      int
	ContentType string
	Text        string
	Title       string
	Links       []anchor
}

func main() {
	dsn := os.Getenv("KNOWLEDGE_DATABASE_URL")
	if dsn == "" {
		log.Fatal("KNOWLEDGE_DATABASE_URL is required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		log.Fatal(err)
	}
	if err := waitForSchema(ctx, db, 90*time.Second); err != nil {
		log.Fatal(err)
	}

	client := &http.Client{
		Timeout: 35 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" || len(via) > 0 && !strings.EqualFold(req.URL.Hostname(), via[0].URL.Hostname()) {
				return errors.New("redirect to a non-HTTPS or different host rejected")
			}
			return nil
		},
	}
	tg := bot.NewTelegram(os.Getenv("TELEGRAM_BOT_TOKEN"))
	log.Print("on-demand source parser started; waiting for an administrator collection request")
	for {
		work, err := claimJob(ctx, db)
		if err != nil {
			log.Printf("claim collection job failed: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		if work == nil {
			time.Sleep(3 * time.Second)
			continue
		}
		log.Printf("collection started: job=%d country=%s limit=%d", work.ID, work.CountryCode, work.Limit)
		discovered, collectErr := collect(ctx, db, client, *work)
		status := "completed"
		errorText := ""
		if collectErr != nil {
			status = "failed"
			errorText = collectErr.Error()
			log.Printf("collection failed: job=%d error=%v", work.ID, collectErr)
		}
		if err := finishJob(ctx, db, work.ID, discovered, status, errorText); err != nil {
			log.Printf("finish collection job failed: job=%d error=%v", work.ID, err)
			continue
		}
		if err := sendReport(ctx, db, tg, work.ID); err != nil {
			log.Printf("send collection report failed: job=%d error=%v", work.ID, err)
		}
	}
}

func waitForSchema(ctx context.Context, db *pgxpool.Pool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var exists bool
		err := db.QueryRow(ctx, `SELECT to_regclass('collection_jobs') IS NOT NULL`).Scan(&exists)
		if err == nil && exists {
			return nil
		}
		time.Sleep(time.Second)
	}
	return errors.New("knowledge database schema was not initialized")
}

func claimJob(ctx context.Context, db *pgxpool.Pool) (*job, error) {
	var j job
	err := db.QueryRow(ctx, `UPDATE collection_jobs
		SET status='running',started_at=now(),finished_at=NULL,error_message=''
		WHERE id=(SELECT id FROM collection_jobs WHERE status='queued' ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING id,destination_code,topic,max_documents,requested_by`).Scan(
		&j.ID, &j.CountryCode, &j.Topic, &j.Limit, &j.RequestedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	err = db.QueryRow(ctx, `SELECT name FROM migration_destinations WHERE code=$1`, j.CountryCode).Scan(&j.Country)
	return &j, err
}

func collect(ctx context.Context, db *pgxpool.Pool, client *http.Client, j job) (int, error) {
	rows, err := db.Query(ctx, `SELECT id,source_url,title,authority,source_language FROM migration_sources
		WHERE destination_code=$1 AND enabled ORDER BY id`, j.CountryCode)
	if err != nil {
		return 0, err
	}
	var seeds []seed
	for rows.Next() {
		var s seed
		if err := rows.Scan(&s.ID, &s.URL, &s.Title, &s.Authority, &s.Language); err != nil {
			rows.Close()
			return 0, err
		}
		seeds = append(seeds, s)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	if len(seeds) == 0 {
		return 0, fmt.Errorf("no official seed pages configured for %s", j.CountryCode)
	}

	allowedHosts := make(map[string]bool)
	queue := make([]candidate, 0, j.Limit)
	seen := make(map[string]bool)
	for _, s := range seeds {
		u, err := safeURL(s.URL)
		if err != nil {
			continue
		}
		allowedHosts[strings.ToLower(u.Hostname())] = true
		key := canonicalURL(u)
		if !seen[key] && len(queue) < j.Limit {
			seen[key] = true
			queue = append(queue, candidate{URL: key, Title: s.Title, Authority: s.Authority, Language: s.Language, Seed: true})
		}
	}
	if len(queue) == 0 {
		return 0, fmt.Errorf("no valid HTTPS seed pages configured for %s", j.CountryCode)
	}

	processed := 0
	for len(queue) > 0 && processed < j.Limit {
		current := queue[0]
		queue = queue[1:]
		processed++
		page, fetchErr := fetch(ctx, client, current.URL)
		if fetchErr != nil {
			statusCode := page.Status
			if err := recordOutcome(ctx, db, j.ID, 0, current.URL, current.Title, "failed", statusCode, fetchErr.Error()); err != nil {
				return len(seen), err
			}
			continue
		}
		if strings.Contains(strings.ToLower(page.ContentType), "pdf") {
			if err := recordOutcome(ctx, db, j.ID, 0, current.URL, current.Title, "unsupported", page.Status, "PDF: извлечение текста пока не подключено"); err != nil {
				return len(seen), err
			}
			continue
		}
		title := page.Title
		if title == "" {
			title = current.Title
		}
		if current.Seed || relevant(j.Topic, title+" "+current.URL+" "+page.Text) {
			outcome, sourceID, saveErr := saveDocument(ctx, db, j, current, title, page)
			if saveErr != nil {
				if err := recordOutcome(ctx, db, j.ID, 0, current.URL, title, "failed", page.Status, saveErr.Error()); err != nil {
					return len(seen), err
				}
			} else if err := recordOutcome(ctx, db, j.ID, sourceID, current.URL, title, outcome, page.Status, ""); err != nil {
				return len(seen), err
			}
		} else if err := recordOutcome(ctx, db, j.ID, 0, current.URL, title, "skipped", page.Status, "Загруженная страница не относится к указанной теме."); err != nil {
			return len(seen), err
		}

		for _, link := range page.Links {
			if len(seen) >= j.Limit {
				break
			}
			u, err := url.Parse(link.URL)
			if err != nil {
				continue
			}
			resolved := currentURL(current.URL).ResolveReference(u)
			safe, err := safeURL(resolved.String())
			if err != nil || !allowedHost(allowedHosts, safe.Hostname()) || !relevant(j.Topic, link.Text+" "+safe.String()) {
				continue
			}
			key := canonicalURL(safe)
			if seen[key] {
				continue
			}
			seen[key] = true
			queue = append(queue, candidate{URL: key, Title: link.Text, Authority: current.Authority, Language: current.Language})
		}
	}
	return len(seen), nil
}

func fetch(ctx context.Context, client *http.Client, rawURL string) (fetchedPage, error) {
	page := fetchedPage{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return page, err
	}
	req.Header.Set("User-Agent", "MigrAI-SourceCollector/1.0 (administrator requested official-source collection)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,application/json,application/pdf;q=0.9")
	resp, err := client.Do(req)
	if err != nil {
		return page, err
	}
	defer resp.Body.Close()
	page.Status = resp.StatusCode
	page.ContentType = resp.Header.Get("Content-Type")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return page, fmt.Errorf("HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return page, err
	}
	if len(data) > maxBodyBytes {
		return page, fmt.Errorf("document exceeds %d bytes", maxBodyBytes)
	}
	body := string(data)
	if strings.Contains(strings.ToLower(page.ContentType), "pdf") {
		return page, nil
	}
	page.Title = extractTitle(body)
	page.Text = normalize(body)
	page.Links = extractLinks(body)
	if page.Text == "" {
		return page, errors.New("no text extracted from page")
	}
	return page, nil
}

func saveDocument(ctx context.Context, db *pgxpool.Pool, j job, c candidate, title string, page fetchedPage) (string, int64, error) {
	if title == "" {
		title = c.URL
	}
	digest := sha256.Sum256([]byte(page.Text))
	hash := hex.EncodeToString(digest[:])
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback(ctx)
	var sourceID int64
	err = tx.QueryRow(ctx, `INSERT INTO migration_sources(destination_code,title,source_url,authority,source_language)
		VALUES($1,$2,$3,$4,$5)
		ON CONFLICT(source_url) DO UPDATE SET title=EXCLUDED.title,authority=EXCLUDED.authority,
			source_language=EXCLUDED.source_language,destination_code=EXCLUDED.destination_code
		RETURNING id`, j.CountryCode, title, c.URL, c.Authority, c.Language).Scan(&sourceID)
	if err != nil {
		return "", 0, err
	}
	var previousID int64
	var previousHash string
	err = tx.QueryRow(ctx, `SELECT id,content_hash FROM source_snapshots WHERE source_id=$1 ORDER BY fetched_at DESC,id DESC LIMIT 1`, sourceID).
		Scan(&previousID, &previousHash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", 0, err
	}
	changed := err == nil && previousHash != hash
	outcome := "new"
	if err == nil && previousHash == hash {
		outcome = "unchanged"
	} else if changed {
		outcome = "changed"
	}
	if outcome != "unchanged" {
		var snapshotID int64
		err = tx.QueryRow(ctx, `INSERT INTO source_snapshots(source_id,http_status,content_type,content_hash,extracted_text,changed)
			VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, sourceID, page.Status, page.ContentType, hash, page.Text, changed).Scan(&snapshotID)
		if err != nil {
			return "", 0, err
		}
		if changed {
			_, err = tx.Exec(ctx, `INSERT INTO source_changes(source_id,old_snapshot_id,new_snapshot_id,status,change_summary)
				VALUES($1,$2,$3,'review_required','Обнаружено изменение официальной страницы; оцените, влияет ли оно на условия переезда.')`, sourceID, previousID, snapshotID)
			if err != nil {
				return "", 0, err
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO knowledge_documents(country,title,source_url,source_type,body,checked_at,source_id,status,content_hash)
			VALUES($1,$2,$3,'official',$4,current_date,$5,'observed',$6)
			ON CONFLICT(country,source_url) DO UPDATE SET title=EXCLUDED.title,body=EXCLUDED.body,
				checked_at=EXCLUDED.checked_at,source_id=EXCLUDED.source_id,status='observed',content_hash=EXCLUDED.content_hash,updated_at=now()`,
			j.Country, title, c.URL, page.Text, sourceID, hash)
		if err != nil {
			return "", 0, err
		}
	} else {
		_, err = tx.Exec(ctx, `UPDATE knowledge_documents SET checked_at=current_date,updated_at=now()
			WHERE source_id=$1 AND status IN ('observed','published')`, sourceID)
		if err != nil {
			return "", 0, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE migration_sources SET last_checked_at=now(),last_status_code=$2,last_error='',enabled=true WHERE id=$1`, sourceID, page.Status)
	if err != nil {
		return "", 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", 0, err
	}
	return outcome, sourceID, nil
}

func recordOutcome(ctx context.Context, db *pgxpool.Pool, jobID, sourceID int64, rawURL, title, outcome string, status int, message string) error {
	var source any
	if sourceID > 0 {
		source = sourceID
	}
	var httpStatus any
	if status > 0 {
		httpStatus = status
	}
	_, err := db.Exec(ctx, `INSERT INTO collection_job_documents(job_id,source_id,document_url,title,outcome,http_status,error_text)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT(job_id,document_url) DO UPDATE SET source_id=EXCLUDED.source_id,title=EXCLUDED.title,
			outcome=EXCLUDED.outcome,http_status=EXCLUDED.http_status,error_text=EXCLUDED.error_text,checked_at=now()`,
		jobID, source, rawURL, title, outcome, httpStatus, message)
	return err
}

func finishJob(ctx context.Context, db *pgxpool.Pool, id int64, discovered int, status, errorText string) error {
	_, err := db.Exec(ctx, `UPDATE collection_jobs j SET status=$2,discovered_count=$3,error_message=$4,finished_at=now(),
		new_count=(SELECT count(*) FROM collection_job_documents d WHERE d.job_id=j.id AND d.outcome='new'),
		changed_count=(SELECT count(*) FROM collection_job_documents d WHERE d.job_id=j.id AND d.outcome='changed'),
		unchanged_count=(SELECT count(*) FROM collection_job_documents d WHERE d.job_id=j.id AND d.outcome='unchanged'),
		skipped_count=(SELECT count(*) FROM collection_job_documents d WHERE d.job_id=j.id AND d.outcome='skipped'),
		failed_count=(SELECT count(*) FROM collection_job_documents d WHERE d.job_id=j.id AND d.outcome IN ('failed','unsupported'))
		WHERE j.id=$1`, id, status, discovered, errorText)
	return err
}

func sendReport(ctx context.Context, db *pgxpool.Pool, tg *bot.Telegram, id int64) error {
	var j bot.CollectionJob
	err := db.QueryRow(ctx, `SELECT j.id,j.requested_by,j.destination_code,d.name,j.topic,j.max_documents,j.status,
		j.discovered_count,j.new_count,j.changed_count,j.unchanged_count,j.skipped_count,j.failed_count,j.error_message
		FROM collection_jobs j JOIN migration_destinations d ON d.code=j.destination_code WHERE j.id=$1`, id).
		Scan(&j.ID, &j.RequestedBy, &j.CountryCode, &j.Country, &j.Topic, &j.MaxDocuments, &j.Status,
			&j.Discovered, &j.New, &j.Changed, &j.Unchanged, &j.Skipped, &j.Failed, &j.Error)
	if err != nil {
		return err
	}
	rows, err := db.Query(ctx, `SELECT document_url,title,outcome,COALESCE(http_status,0),error_text,checked_at::text
		FROM collection_job_documents WHERE job_id=$1 ORDER BY checked_at,id`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	var documents []bot.CollectionDocument
	for rows.Next() {
		var d bot.CollectionDocument
		if err := rows.Scan(&d.URL, &d.Title, &d.Outcome, &d.HTTPStatus, &d.Error, &d.CheckedAt); err != nil {
			return err
		}
		documents = append(documents, d)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	caption := fmt.Sprintf("Сбор %d завершён: %s, %s. Найдено страниц: %d; обработано: %d; новых: %d; изменилось: %d; без изменений: %d; пропущено: %d; ошибок: %d.",
		j.ID, j.Country, j.Topic, j.Discovered, len(documents), j.New, j.Changed, j.Unchanged, j.Skipped, j.Failed)
	if j.Status == "failed" && j.Error != "" {
		caption += " Ошибка: " + j.Error
	}
	return tg.SendDocument(ctx, j.RequestedBy, fmt.Sprintf("migrai-sources-%d.csv", j.ID), caption, bot.EncodeCollectionCSV(j, documents))
}

func safeURL(raw string) (*url.URL, error) {
	u, err := url.ParseRequestURI(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("only valid HTTPS source URLs are allowed")
	}
	return u, nil
}

func canonicalURL(u *url.URL) string {
	copy := *u
	copy.Fragment = ""
	query := copy.Query()
	for key := range query {
		if strings.HasPrefix(strings.ToLower(key), "utm_") || strings.EqualFold(key, "fbclid") || strings.EqualFold(key, "gclid") {
			query.Del(key)
		}
	}
	copy.RawQuery = query.Encode()
	return copy.String()
}

func currentURL(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}

func allowedHost(allowed map[string]bool, host string) bool {
	host = strings.ToLower(host)
	if allowed[host] {
		return true
	}
	for seedHost := range allowed {
		if strings.HasSuffix(host, "."+seedHost) {
			return true
		}
	}
	return false
}

func extractTitle(body string) string {
	match := titleTag.FindStringSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return normalize(match[1])
}

func extractLinks(body string) []anchor {
	matches := anchorTag.FindAllStringSubmatch(body, -1)
	links := make([]anchor, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		links = append(links, anchor{URL: html.UnescapeString(match[1]), Text: normalize(match[2])})
	}
	return links
}

func relevant(topic, text string) bool {
	topic = strings.ToLower(strings.TrimSpace(topic))
	text = strings.ToLower(text)
	var terms []string
	add := func(values ...string) { terms = append(terms, values...) }
	if strings.Contains(topic, "рег") {
		add("регистрац", "registration", "register", "notify", "address")
	}
	if strings.Contains(topic, "внж") || strings.Contains(topic, "residen") || strings.Contains(topic, "прожив") {
		add("внж", "residence permit", "temporary residence", "permanent residence", "residence", "stay permit")
	}
	if strings.Contains(topic, "виз") || strings.Contains(topic, "visa") || strings.Contains(topic, "въезд") {
		add("виза", "visa", "evisa", "entry permit", "entry requirement")
	}
	if strings.Contains(topic, "работ") || strings.Contains(topic, "work") || strings.Contains(topic, "труд") {
		add("работ", "work permit", "work visa", "employment", "labour", "labor")
	}
	if strings.Contains(topic, "учеб") || strings.Contains(topic, "study") {
		add("учеб", "student visa", "study", "education")
	}
	if strings.Contains(topic, "сем") || strings.Contains(topic, "family") {
		add("сем", "family", "dependent", "spouse", "reunification")
	}
	if strings.Contains(topic, "бизнес") || strings.Contains(topic, "business") {
		add("бизнес", "business", "entrepreneur", "startup", "self-employed")
	}
	if strings.Contains(topic, "закон") || strings.Contains(topic, "legislation") || strings.Contains(topic, "law") {
		add("закон", "law", "legislation", "regulation", "act", "decree", "statute")
	}
	if strings.Contains(topic, "программ") || strings.Contains(topic, "program") || strings.Contains(topic, "programme") {
		add("программ", "program", "programme", "scheme", "pathway")
	}
	if strings.Contains(topic, "документ") || strings.Contains(topic, "document") {
		add("документ", "document", "application", "requirements")
	}
	if topic == "все" || topic == "всё" || topic == "all" || topic == "релокация" {
		add("visa", "residence", "immigration", "registration", "work permit", "study", "family")
	}
	if len(terms) == 0 {
		for _, term := range strings.Fields(topic) {
			if len([]rune(term)) >= 4 {
				terms = append(terms, term)
			}
		}
	}
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func normalize(in string) string {
	lower := strings.ToLower(strings.TrimSpace(in))
	if strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype html") {
		in = scriptStyle.ReplaceAllString(in, " ")
		in = tags.ReplaceAllString(in, " ")
	}
	in = html.UnescapeString(in)
	return strings.TrimSpace(whitespace.ReplaceAllString(in, " "))
}
