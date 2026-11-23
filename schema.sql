CREATE TABLE IF NOT EXISTS users (
  telegram_id BIGINT PRIMARY KEY,
  username TEXT NOT NULL DEFAULT '',
  profile JSONB NOT NULL DEFAULT '{}'::jsonb,
  profile_confirmed BOOLEAN NOT NULL DEFAULT FALSE,
  awaiting_confirmation BOOLEAN NOT NULL DEFAULT FALSE,
  interview_question TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE users ADD COLUMN IF NOT EXISTS awaiting_confirmation BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS interview_question TEXT NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS messages (
  id BIGSERIAL PRIMARY KEY,
  telegram_id BIGINT NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('user','assistant')),
  content TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS messages_user_created ON messages(telegram_id, created_at DESC);

ALTER TABLE users ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS chat_id BIGINT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_seen TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS feedback_mode BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS contact_consent BOOLEAN NOT NULL DEFAULT false;
CREATE TABLE IF NOT EXISTS conversation_events (
 id BIGSERIAL PRIMARY KEY,
 telegram_id BIGINT NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
 kind TEXT NOT NULL,
 content TEXT NOT NULL,
 message_id BIGINT UNIQUE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS conversation_events_user_date ON conversation_events(telegram_id,created_at DESC,id DESC);
UPDATE users u SET last_seen=COALESCE((SELECT max(created_at) FROM messages m WHERE m.telegram_id=u.telegram_id AND m.role='user'),u.updated_at) WHERE last_seen IS NULL;
CREATE TABLE IF NOT EXISTS app_migrations (name TEXT PRIMARY KEY);
WITH first_run AS (INSERT INTO app_migrations(name) VALUES('admin_history_v1') ON CONFLICT DO NOTHING RETURNING name)
INSERT INTO conversation_events(telegram_id,kind,content,message_id,created_at)
 SELECT telegram_id,role,left(content,350),id,created_at FROM messages
 WHERE created_at >= now()-interval '90 days' AND EXISTS(SELECT 1 FROM first_run)
 ON CONFLICT(message_id) DO NOTHING;
