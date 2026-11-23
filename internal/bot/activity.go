package bot

import (
	"context"
	"strings"
	"time"
)

func excerpt(s string) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > 350 {
		return string(r[:349]) + "…"
	}
	return string(r)
}
func (s *Store) RecordIncoming(ctx context.Context, m *Message, text string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `UPDATE users SET display_name=$2,chat_id=$3,last_seen=now() WHERE telegram_id=$1`, m.From.ID, strings.TrimSpace(m.From.FirstName+" "+m.From.LastName), m.Chat.ID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO conversation_events(telegram_id,kind,content) VALUES($1,'user',$2)`, m.From.ID, excerpt(text))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) RecordSent(ctx context.Context, chatID int64, text string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO conversation_events(telegram_id,kind,content) SELECT telegram_id,'assistant',$2 FROM users WHERE chat_id=$1`, chatID, excerpt(text))
	return err
}
func (s *Store) RecordFeedback(ctx context.Context, id int64, kind, text string, wait bool) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO conversation_events(telegram_id,kind,content) VALUES($1,$2,$3)`, id, kind, excerpt(text))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE users SET feedback_mode=$2 WHERE telegram_id=$1`, id, wait)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) FeedbackPending(ctx context.Context, id int64) (bool, error) {
	var v bool
	err := s.db.QueryRow(ctx, `SELECT feedback_mode FROM users WHERE telegram_id=$1`, id).Scan(&v)
	return v, err
}
func (s *Store) ShareContact(ctx context.Context, id int64, phone string) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET phone=$2,contact_consent=true,feedback_mode=false WHERE telegram_id=$1`, id, phone)
	return err
}
func (s *Store) PurgeContext(ctx context.Context) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, table := range []string{"conversation_events", "messages"} {
		if _, err = tx.Exec(ctx, "DELETE FROM "+table+" WHERE created_at < now()-interval '90 days'"); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) MaintainRetention(ctx context.Context, report func(error)) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if err := s.PurgeContext(ctx); err != nil {
			report(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
