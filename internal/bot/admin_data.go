package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

const adminPeople = `WITH people AS (
 SELECT u.*, CASE WHEN NOT profile_confirmed AND awaiting_confirmation THEN 'review'
 WHEN NOT profile_confirmed THEN 'survey'
 WHEN EXISTS(SELECT 1 FROM messages m WHERE m.telegram_id=u.telegram_id AND m.role='assistant') THEN 'chat'
 ELSE 'ready' END AS stage
 FROM users u
), selected AS (SELECT * FROM people WHERE (($1 LIKE 'id:%' AND telegram_id::text=substring($1 from 4)) OR ($1 NOT LIKE 'id:%' AND ($1='' OR display_name ILIKE '%'||$1||'%' OR username ILIKE '%'||$1||'%' OR telegram_id::text=$1 OR profile::text ILIKE '%'||$1||'%'))) AND ($2='' OR stage=$2)) `

func (s *Store) AdminUsers(ctx context.Context, search, stage string, offset, limit int) ([]json.RawMessage, int, error) {
	var total int
	if err := s.db.QueryRow(ctx, adminPeople+`SELECT count(*) FROM selected`, search, stage).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(ctx, adminPeople+`SELECT jsonb_build_object(
 'id',u.telegram_id::text,'name',u.display_name,'username',u.username,'chat_id',u.chat_id::text,'phone',u.phone,
 'profile',u.profile,'stage',u.stage,'question',u.interview_question,'first_seen',u.created_at,'last_seen',u.last_seen,
 'contact_consent',u.contact_consent,
 'message_count',(SELECT count(*) FROM conversation_events e WHERE e.telegram_id=u.telegram_id AND e.kind='user' AND e.created_at>=now()-interval '90 days'),
 'last_question',(SELECT content FROM conversation_events e WHERE e.telegram_id=u.telegram_id AND e.kind='user' AND e.created_at>=now()-interval '90 days' ORDER BY created_at DESC,id DESC LIMIT 1),
 'last_answer',(SELECT content FROM conversation_events e WHERE e.telegram_id=u.telegram_id AND e.kind='assistant' AND e.created_at>=now()-interval '90 days' ORDER BY created_at DESC,id DESC LIMIT 1),
 'feedback',(SELECT content FROM conversation_events e WHERE e.telegram_id=u.telegram_id AND e.kind IN ('rating','feedback') AND e.created_at>=now()-interval '90 days' ORDER BY created_at DESC,id DESC LIMIT 1)
 ) FROM selected u ORDER BY last_seen DESC NULLS LAST,telegram_id LIMIT $3 OFFSET $4`, search, stage, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, 0, err
		}
		result = append(result, json.RawMessage(raw))
	}
	return result, total, rows.Err()
}
func (s *Store) AdminEvents(ctx context.Context, id string, offset int) ([]json.RawMessage, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid user")
	}
	rows, err := s.db.Query(ctx, `SELECT jsonb_build_object('id',id::text,'kind',kind,'text',content,'at',created_at) FROM conversation_events WHERE telegram_id=$1 AND created_at>=now()-interval '90 days' ORDER BY created_at DESC,id DESC LIMIT 100 OFFSET $2`, n, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, rows.Err()
}
