package bot

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	db        *pgxpool.Pool
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

func OpenStore(ctx context.Context, dsn string) (*Store, error) {
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	db, err := openPool(ctx, dsn)
	if err != nil {
		return nil, err
	}
	schema, err := os.ReadFile("schema.sql")
	if err == nil {
		_, err = db.Exec(ctx, string(schema))
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize application database: %w", err)
	}
	return &Store{db: db}, nil
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

func (s *Store) SaveSurvey(ctx context.Context,id int64,p map[string]string,key string) error {
 raw,err:=json.Marshal(p);if err!=nil{return err}
 _,err=s.db.Exec(ctx,`UPDATE users SET profile=$2,profile_confirmed=false,awaiting_confirmation=$3,interview_question=$4,updated_at=now() WHERE telegram_id=$1`,id,raw,key=="","survey:"+key)
 return err
}
