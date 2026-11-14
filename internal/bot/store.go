package bot

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	db        *pgxpool.Pool
	knowledge *pgxpool.Pool
}

type User struct {
	Profile           map[string]string
	Confirmed, Awaiting bool
	InterviewQuestion string
}

type CollectionJob struct {
	ID, RequestedBy                       int64
	CountryCode, Country, Topic, Status   string
	MaxDocuments                          int
	Discovered, New, Changed, Unchanged   int
	Skipped                               int
	Failed                                int
	Error                                 string
}

type CollectionDocument struct {
	URL, Title, Outcome, Error, CheckedAt string
	HTTPStatus                           int
}

type CatalogSource struct {
	CountryCode, Country, Title, URL, Authority string
	CheckedAt, LastError                       string
	HTTPStatus                                 int
}

type CountrySourceCount struct {
	CountryCode, Country string
	Count                int
}

func OpenStore(ctx context.Context, dsn, knowledgeDSN string) (*Store, error) {
	if dsn == "" || knowledgeDSN == "" {
		return nil, fmt.Errorf("DATABASE_URL and KNOWLEDGE_DATABASE_URL are required")
	}
	db, err := openPool(ctx, dsn)
	if err != nil {
		return nil, err
	}
	appSchema, err := os.ReadFile("schema.sql")
	if err == nil {
		_, err = db.Exec(ctx, string(appSchema))
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize application database: %w", err)
	}
	knowledge, err := openPool(ctx, knowledgeDSN)
	if err != nil {
		db.Close()
		return nil, err
	}
	schema, err := os.ReadFile("knowledge_schema.sql")
	if err == nil {
		_, err = knowledge.Exec(ctx, string(schema))
	}
	if err != nil {
		knowledge.Close()
		db.Close()
		return nil, fmt.Errorf("initialize knowledge database: %w", err)
	}
	if err := migrateLegacyKnowledge(ctx, db, knowledge); err != nil {
		knowledge.Close()
		db.Close()
		return nil, fmt.Errorf("migrate existing knowledge data: %w", err)
	}
	return &Store{db: db, knowledge: knowledge}, nil
}

func migrateLegacyKnowledge(ctx context.Context, source, target *pgxpool.Pool) error {
	var applied bool
	if err := target.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_migrations WHERE name='split-knowledge-db-v1')`).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}
	var hasSources bool
	if err := source.QueryRow(ctx, `SELECT to_regclass('migration_sources') IS NOT NULL`).Scan(&hasSources); err != nil {
		return err
	}
	if !hasSources {
		_, err := target.Exec(ctx, `INSERT INTO knowledge_migrations(name) VALUES('split-knowledge-db-v1') ON CONFLICT DO NOTHING`)
		return err
	}

	tx, err := target.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	destinations, err := source.Query(ctx, `SELECT code,name,region,enabled,created_at FROM migration_destinations ORDER BY code`)
	if err != nil {
		return err
	}
	for destinations.Next() {
		var code, name, region string
		var enabled bool
		var created time.Time
		if err := destinations.Scan(&code, &name, &region, &enabled, &created); err != nil {
			destinations.Close()
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO migration_destinations(code,name,region,enabled,created_at) VALUES($1,$2,$3,$4,$5)
			ON CONFLICT(code) DO UPDATE SET name=EXCLUDED.name,region=EXCLUDED.region,enabled=EXCLUDED.enabled`, code, name, region, enabled, created); err != nil {
			destinations.Close()
			return err
		}
	}
	if err := destinations.Err(); err != nil {
		destinations.Close()
		return err
	}
	destinations.Close()

	sourceIDs := make(map[int64]int64)
	sources, err := source.Query(ctx, `SELECT id,destination_code,title,source_url,authority,source_language,enabled,
		check_interval_days,last_checked_at,last_status_code,last_error,created_at FROM migration_sources ORDER BY id`)
	if err != nil {
		return err
	}
	for sources.Next() {
		var oldID int64
		var destinationCode *string
		var title, sourceURL, authority, language, lastError string
		var enabled bool
		var interval int
		var checkedAt *time.Time
		var statusCode *int
		var created time.Time
		if err := sources.Scan(&oldID, &destinationCode, &title, &sourceURL, &authority, &language, &enabled,
			&interval, &checkedAt, &statusCode, &lastError, &created); err != nil {
			sources.Close()
			return err
		}
		var newID int64
		err := tx.QueryRow(ctx, `INSERT INTO migration_sources(destination_code,title,source_url,authority,source_language,
			enabled,check_interval_days,last_checked_at,last_status_code,last_error,created_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT(source_url) DO UPDATE SET title=EXCLUDED.title,authority=EXCLUDED.authority,
				source_language=EXCLUDED.source_language,enabled=EXCLUDED.enabled,check_interval_days=EXCLUDED.check_interval_days,
				last_checked_at=EXCLUDED.last_checked_at,last_status_code=EXCLUDED.last_status_code,last_error=EXCLUDED.last_error
			RETURNING id`, destinationCode, title, sourceURL, authority, language, enabled, interval, checkedAt, statusCode, lastError, created).Scan(&newID)
		if err != nil {
			sources.Close()
			return err
		}
		sourceIDs[oldID] = newID
	}
	if err := sources.Err(); err != nil {
		sources.Close()
		return err
	}
	sources.Close()

	var hasSnapshots bool
	if err := source.QueryRow(ctx, `SELECT to_regclass('source_snapshots') IS NOT NULL`).Scan(&hasSnapshots); err != nil {
		return err
	}
	if hasSnapshots {
		rows, err := source.Query(ctx, `SELECT id,source_id,fetched_at,http_status,content_type,content_hash,extracted_text,changed
			FROM source_snapshots ORDER BY id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, oldSourceID int64
			var fetchedAt time.Time
			var status int
			var contentType, hash, text string
			var changed bool
			if err := rows.Scan(&id, &oldSourceID, &fetchedAt, &status, &contentType, &hash, &text, &changed); err != nil {
				rows.Close()
				return err
			}
			newSourceID, ok := sourceIDs[oldSourceID]
			if !ok {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO source_snapshots(id,source_id,fetched_at,http_status,content_type,content_hash,extracted_text,changed)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO NOTHING`, id, newSourceID, fetchedAt, status, contentType, hash, text, changed); err != nil {
				rows.Close()
				return err
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if _, err := tx.Exec(ctx, `SELECT setval(pg_get_serial_sequence('source_snapshots','id'),
			GREATEST(COALESCE((SELECT MAX(id) FROM source_snapshots),1),1),(SELECT count(*)>0 FROM source_snapshots))`); err != nil {
			return err
		}
	}

	var hasChanges bool
	if err := source.QueryRow(ctx, `SELECT to_regclass('source_changes') IS NOT NULL`).Scan(&hasChanges); err != nil {
		return err
	}
	if hasChanges {
		rows, err := source.Query(ctx, `SELECT id,source_id,old_snapshot_id,new_snapshot_id,status,change_summary,detected_at,reviewed_at
			FROM source_changes ORDER BY id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, oldSourceID, newSnapshotID int64
			var oldSnapshotID *int64
			var status, summary string
			var detectedAt time.Time
			var reviewedAt *time.Time
			if err := rows.Scan(&id, &oldSourceID, &oldSnapshotID, &newSnapshotID, &status, &summary, &detectedAt, &reviewedAt); err != nil {
				rows.Close()
				return err
			}
			newSourceID, ok := sourceIDs[oldSourceID]
			if !ok {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO source_changes(id,source_id,old_snapshot_id,new_snapshot_id,status,change_summary,detected_at,reviewed_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO NOTHING`, id, newSourceID, oldSnapshotID, newSnapshotID, status, summary, detectedAt, reviewedAt); err != nil {
				rows.Close()
				return err
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if _, err := tx.Exec(ctx, `SELECT setval(pg_get_serial_sequence('source_changes','id'),
			GREATEST(COALESCE((SELECT MAX(id) FROM source_changes),1),1),(SELECT count(*)>0 FROM source_changes))`); err != nil {
			return err
		}
	}

	var hasSignals bool
	if err := source.QueryRow(ctx, `SELECT to_regclass('destination_signals') IS NOT NULL`).Scan(&hasSignals); err != nil {
		return err
	}
	if hasSignals {
		rows, err := source.Query(ctx, `SELECT destination_code,signal_type,audience_scope,summary,source_type,source_url,checked_at,status,valid_from,valid_until,created_at,updated_at
			FROM destination_signals ORDER BY id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var code, kind, scope, summary, sourceType, sourceURL, status string
			var checkedAt, createdAt, updatedAt time.Time
			var validFrom, validUntil *time.Time
			if err := rows.Scan(&code, &kind, &scope, &summary, &sourceType, &sourceURL, &checkedAt, &status, &validFrom, &validUntil, &createdAt, &updatedAt); err != nil {
				rows.Close()
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO destination_signals(destination_code,signal_type,audience_scope,summary,source_type,source_url,checked_at,status,valid_from,valid_until,created_at,updated_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
				ON CONFLICT(destination_code,signal_type,source_url) DO UPDATE SET summary=EXCLUDED.summary,checked_at=EXCLUDED.checked_at,status=EXCLUDED.status,
					valid_from=EXCLUDED.valid_from,valid_until=EXCLUDED.valid_until,updated_at=EXCLUDED.updated_at`,
				code, kind, scope, summary, sourceType, sourceURL, checkedAt, status, validFrom, validUntil, createdAt, updatedAt); err != nil {
				rows.Close()
				return err
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}

	var hasDocuments bool
	if err := source.QueryRow(ctx, `SELECT to_regclass('knowledge_documents') IS NOT NULL`).Scan(&hasDocuments); err != nil {
		return err
	}
	if hasDocuments {
		rows, err := source.Query(ctx, `SELECT country,title,source_url,source_type,body,checked_at,source_id,status,valid_from,valid_until,content_hash,created_at,updated_at
			FROM knowledge_documents ORDER BY id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var country, title, sourceURL, sourceType, body, status, hash string
			var checkedAt, createdAt, updatedAt time.Time
			var oldSourceID *int64
			var validFrom, validUntil *time.Time
			if err := rows.Scan(&country, &title, &sourceURL, &sourceType, &body, &checkedAt, &oldSourceID, &status, &validFrom, &validUntil, &hash, &createdAt, &updatedAt); err != nil {
				rows.Close()
				return err
			}
			var newSourceID *int64
			if oldSourceID != nil {
				if id, ok := sourceIDs[*oldSourceID]; ok {
					newSourceID = &id
				}
			}
			if _, err := tx.Exec(ctx, `INSERT INTO knowledge_documents(country,title,source_url,source_type,body,checked_at,source_id,status,valid_from,valid_until,content_hash,created_at,updated_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
				ON CONFLICT(country,source_url) DO UPDATE SET title=EXCLUDED.title,source_type=EXCLUDED.source_type,body=EXCLUDED.body,
					checked_at=EXCLUDED.checked_at,source_id=EXCLUDED.source_id,status=EXCLUDED.status,valid_from=EXCLUDED.valid_from,
					valid_until=EXCLUDED.valid_until,content_hash=EXCLUDED.content_hash,updated_at=EXCLUDED.updated_at`,
				country, title, sourceURL, sourceType, body, checkedAt, newSourceID, status, validFrom, validUntil, hash, createdAt, updatedAt); err != nil {
				rows.Close()
				return err
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}

	if _, err := tx.Exec(ctx, `INSERT INTO knowledge_migrations(name) VALUES('split-knowledge-db-v1') ON CONFLICT DO NOTHING`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func openPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s *Store) Close() {
	s.db.Close()
	s.knowledge.Close()
}

func (s *Store) Get(ctx context.Context, id int64, username string) (User, error) {
	var raw []byte
	var u User
	err := s.db.QueryRow(ctx, `INSERT INTO users(telegram_id,username) VALUES($1,$2)
		ON CONFLICT(telegram_id) DO UPDATE SET username=EXCLUDED.username,updated_at=now()
		RETURNING profile,profile_confirmed,awaiting_confirmation,interview_question`, id, username).
		Scan(&raw, &u.Confirmed, &u.Awaiting, &u.InterviewQuestion)
	if err != nil {
		return u, err
	}
	if err = json.Unmarshal(raw, &u.Profile); err != nil {
		return u, err
	}
	if u.Profile == nil {
		u.Profile = map[string]string{}
	}
	return u, nil
}

func (s *Store) SaveProfile(ctx context.Context, id int64, profile map[string]string, awaiting bool) error {
	raw, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `UPDATE users SET profile=$2,awaiting_confirmation=$3,updated_at=now() WHERE telegram_id=$1`, id, raw, awaiting)
	return err
}

func (s *Store) SetInterviewQuestion(ctx context.Context, id int64, question string) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET interview_question=$2,updated_at=now() WHERE telegram_id=$1`, id, question)
	return err
}

func (s *Store) BeginInterview(ctx context.Context, id int64, question string) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET profile_confirmed=false,awaiting_confirmation=false,interview_question=$2,updated_at=now() WHERE telegram_id=$1`, id, question)
	return err
}

func (s *Store) Confirm(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET profile_confirmed=true,awaiting_confirmation=false,interview_question='',updated_at=now() WHERE telegram_id=$1`, id)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM users WHERE telegram_id=$1`, id)
	return err
}

func (s *Store) AddMessage(ctx context.Context, id int64, role, text string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO messages(telegram_id,role,content) VALUES($1,$2,$3)`, id, role, text)
	return err
}

func (s *Store) History(ctx context.Context, id int64, limit int) ([]ChatMessage, error) {
	rows, err := s.db.Query(ctx, `SELECT role,content FROM (
		SELECT role,content,created_at,id FROM messages WHERE telegram_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2
	) q ORDER BY created_at,id`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err = rows.Scan(&m.Role, &m.Content); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) Knowledge(ctx context.Context, profile map[string]string) (string, error) {
	var terms []string
	for _, v := range strings.Split(strings.ToLower(profile["countries"]), ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			terms = append(terms, v)
		}
	}
	rows, err := s.knowledge.Query(ctx, `WITH ranked AS (
		SELECT country,title,source_url,left(body,5000) AS body,checked_at,status,
		       row_number() OVER (PARTITION BY country ORDER BY checked_at DESC,id DESC) AS country_rank
		FROM knowledge_documents
		WHERE status IN ('observed','published')
		  AND (valid_from IS NULL OR valid_from<=current_date)
		  AND (valid_until IS NULL OR valid_until>=current_date)
		  AND checked_at>=current_date-14
		  AND body<>''
		  AND (cardinality($1::text[])=0 OR lower(country)=ANY($1))
	)
	SELECT country,title,source_url,body,checked_at::text,status FROM ranked
	WHERE country_rank<=2 ORDER BY country,checked_at DESC LIMIT 18`, terms)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var country, title, sourceURL, body, date, status string
		if err = rows.Scan(&country, &title, &sourceURL, &body, &date, &status); err != nil {
			return "", err
		}
		b.WriteString("\n[" + country + "] " + title + "; checked " + date + "; " + status + "; source URL: " + sourceURL + "\n" + body + "\n")
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	rows.Close()

	signals, err := s.knowledge.Query(ctx, `WITH ranked AS (
		SELECT d.name,s.signal_type,s.audience_scope,s.summary,s.source_type,s.source_url,s.checked_at,s.status,
		       row_number() OVER (PARTITION BY d.code,s.signal_type ORDER BY s.checked_at DESC,s.id DESC) AS signal_rank
		FROM destination_signals s JOIN migration_destinations d ON d.code=s.destination_code
		WHERE s.status IN ('observed','published')
		  AND s.checked_at>=current_date-90
		  AND (s.valid_from IS NULL OR s.valid_from<=current_date)
		  AND (s.valid_until IS NULL OR s.valid_until>=current_date)
		  AND (cardinality($1::text[])=0 OR lower(d.name)=ANY($1))
	)
	SELECT name,signal_type,audience_scope,summary,source_type,source_url,checked_at::text,status FROM ranked
	WHERE signal_rank=1 ORDER BY name,signal_type LIMIT 36`, terms)
	if err != nil {
		return "", err
	}
	defer signals.Close()
	for signals.Next() {
		var country, kind, scope, summary, sourceType, sourceURL, date, status string
		if err = signals.Scan(&country, &kind, &scope, &summary, &sourceType, &sourceURL, &date, &status); err != nil {
			return "", err
		}
		b.WriteString("\n[" + country + "] indicator=" + kind + "; scope=" + scope + "; source_type=" + sourceType + "; checked " + date + "; " + status + "; source URL: " + sourceURL + "\n" + summary + "\n")
	}
	return b.String(), signals.Err()
}

func (s *Store) QueueCollection(ctx context.Context, countryCode, topic string, limit int, requesterID int64) (int64, error) {
	if limit < 1 || limit > 100 {
		return 0, fmt.Errorf("collection limit must be between 1 and 100")
	}
	var id int64
	err := s.knowledge.QueryRow(ctx, `INSERT INTO collection_jobs(destination_code,topic,max_documents,requested_by)
		VALUES($1,$2,$3,$4) RETURNING id`, countryCode, topic, limit, requesterID).Scan(&id)
	return id, err
}

func (s *Store) CollectionJob(ctx context.Context, id int64) (CollectionJob, error) {
	var job CollectionJob
	err := s.knowledge.QueryRow(ctx, `SELECT j.id,j.requested_by,j.destination_code,d.name,j.topic,j.max_documents,j.status,
		j.discovered_count,j.new_count,j.changed_count,j.unchanged_count,j.skipped_count,j.failed_count,j.error_message
		FROM collection_jobs j JOIN migration_destinations d ON d.code=j.destination_code WHERE j.id=$1`, id).
		Scan(&job.ID, &job.RequestedBy, &job.CountryCode, &job.Country, &job.Topic, &job.MaxDocuments, &job.Status,
			&job.Discovered, &job.New, &job.Changed, &job.Unchanged, &job.Skipped, &job.Failed, &job.Error)
	return job, err
}

func (s *Store) CollectionDocuments(ctx context.Context, jobID int64) ([]CollectionDocument, error) {
	rows, err := s.knowledge.Query(ctx, `SELECT document_url,title,outcome,COALESCE(http_status,0),error_text,checked_at::text
		FROM collection_job_documents WHERE job_id=$1 ORDER BY checked_at,id`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var docs []CollectionDocument
	for rows.Next() {
		var d CollectionDocument
		if err := rows.Scan(&d.URL, &d.Title, &d.Outcome, &d.HTTPStatus, &d.Error, &d.CheckedAt); err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}

func EncodeCollectionCSV(job CollectionJob, docs []CollectionDocument) []byte {
	var out bytes.Buffer
	out.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&out)
	_ = w.Write([]string{"Сводка сбора", "Значение"})
	_ = w.Write([]string{"ID запуска", fmt.Sprint(job.ID)})
	_ = w.Write([]string{"Страна", job.Country})
	_ = w.Write([]string{"Тема", job.Topic})
	_ = w.Write([]string{"Статус", job.Status})
	_ = w.Write([]string{"Лимит страниц", fmt.Sprint(job.MaxDocuments)})
	_ = w.Write([]string{"Обработано", fmt.Sprint(job.Discovered)})
	_ = w.Write([]string{"Новых", fmt.Sprint(job.New)})
	_ = w.Write([]string{"Изменилось", fmt.Sprint(job.Changed)})
	_ = w.Write([]string{"Без изменений", fmt.Sprint(job.Unchanged)})
	_ = w.Write([]string{"Пропущено", fmt.Sprint(job.Skipped)})
	_ = w.Write([]string{"Ошибок", fmt.Sprint(job.Failed)})
	_ = w.Write([]string{})
	_ = w.Write([]string{"URL", "Название", "Результат", "HTTP", "Ошибка", "Проверено"})
	for _, d := range docs {
		_ = w.Write([]string{d.URL, d.Title, d.Outcome, fmt.Sprint(d.HTTPStatus), d.Error, d.CheckedAt})
	}
	w.Flush()
	return out.Bytes()
}

func (s *Store) SourceCatalog(ctx context.Context) ([]CountrySourceCount, []CatalogSource, error) {
	countRows, err := s.knowledge.Query(ctx, `SELECT d.code,d.name,count(ms.id)
		FROM migration_destinations d LEFT JOIN migration_sources ms ON ms.destination_code=d.code
		GROUP BY d.code,d.name ORDER BY d.name`)
	if err != nil {
		return nil, nil, err
	}
	var counts []CountrySourceCount
	for countRows.Next() {
		var c CountrySourceCount
		if err := countRows.Scan(&c.CountryCode, &c.Country, &c.Count); err != nil {
			countRows.Close()
			return nil, nil, err
		}
		counts = append(counts, c)
	}
	if err := countRows.Err(); err != nil {
		countRows.Close()
		return nil, nil, err
	}
	countRows.Close()

	rows, err := s.knowledge.Query(ctx, `SELECT d.code,d.name,s.title,s.source_url,s.authority,
		COALESCE(s.last_checked_at::text,''),COALESCE(s.last_status_code,0),COALESCE(s.last_error,'')
		FROM migration_sources s JOIN migration_destinations d ON d.code=s.destination_code
		ORDER BY d.name,s.title,s.source_url`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var sources []CatalogSource
	for rows.Next() {
		var source CatalogSource
		if err := rows.Scan(&source.CountryCode, &source.Country, &source.Title, &source.URL, &source.Authority,
			&source.CheckedAt, &source.HTTPStatus, &source.LastError); err != nil {
			return nil, nil, err
		}
		sources = append(sources, source)
	}
	return counts, sources, rows.Err()
}

func EncodeSourceCatalogCSV(counts []CountrySourceCount, sources []CatalogSource) []byte {
	var out bytes.Buffer
	out.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&out)
	_ = w.Write([]string{"Источники по странам", "Количество"})
	var total int
	for _, c := range counts {
		_ = w.Write([]string{c.Country + " (" + c.CountryCode + ")", fmt.Sprint(c.Count)})
		total += c.Count
	}
	_ = w.Write([]string{"Всего источников", fmt.Sprint(total)})
	_ = w.Write([]string{})
	_ = w.Write([]string{"Страна", "Код", "Название страницы", "Ссылка", "Ведомство", "Последняя проверка", "HTTP", "Последняя ошибка"})
	for _, source := range sources {
		_ = w.Write([]string{source.Country, source.CountryCode, source.Title, source.URL, source.Authority,
			source.CheckedAt, fmt.Sprint(source.HTTPStatus), source.LastError})
	}
	w.Flush()
	return out.Bytes()
}
