CREATE TABLE IF NOT EXISTS migration_destinations (
  code TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  region TEXT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO migration_destinations(code,name,region) VALUES
  ('AM','Армения','Европа и Кавказ'),
  ('GE','Грузия','Европа и Кавказ'),
  ('RS','Сербия','Европа и Балканы'),
  ('KZ','Казахстан','Центральная Азия'),
  ('TR','Турция','Европа и Азия'),
  ('DE','Германия','Европа'),
  ('ES','Испания','Европа'),
  ('VN','Вьетнам','Азия'),
  ('TH','Таиланд','Азия')
ON CONFLICT(code) DO UPDATE SET name=EXCLUDED.name,region=EXCLUDED.region;

CREATE TABLE IF NOT EXISTS migration_sources (
  id BIGSERIAL PRIMARY KEY,
  destination_code TEXT REFERENCES migration_destinations(code),
  title TEXT NOT NULL,
  source_url TEXT NOT NULL UNIQUE,
  authority TEXT NOT NULL,
  source_language TEXT NOT NULL DEFAULT 'en',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  check_interval_days INTEGER NOT NULL DEFAULT 7 CHECK(check_interval_days > 0),
  last_checked_at TIMESTAMPTZ,
  last_status_code INTEGER,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE migration_sources ADD COLUMN IF NOT EXISTS last_status_code INTEGER;
ALTER TABLE migration_sources ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';
UPDATE migration_sources SET enabled=FALSE
WHERE source_url IN ('https://vmp.gov.kz/en/services/visa-service','https://evisa.gov.vn/');
INSERT INTO migration_sources(destination_code,title,source_url,authority,source_language) VALUES
  ('AM','Визы и въезд — МИД Армении','https://www.mfa.am/en/visa/','Министерство иностранных дел Армении','en'),
  ('GE','Въезд и визовая информация — Geoconsul','https://geoconsul.gov.ge/en/entering-georgia','Консульский департамент МИД Грузии','en'),
  ('RS','Временное проживание — Welcome to Serbia','https://welcometoserbia.gov.rs/temporary-residence','Портал для иностранных граждан Сербии','en'),
  ('KZ','Информация о получении электронной визы — gov.kz','https://www.gov.kz/situations/763/1627?lang=en','Правительство Республики Казахстан','en'),
  ('TR','Визовая информация для иностранцев — МИД Турции','https://www.mfa.gov.tr/visa-information-for-foreigners.en.mfa','Министерство иностранных дел Турции','en'),
  ('DE','Визы и признание квалификаций — Make it in Germany','https://www.make-it-in-germany.com/en/visa-recognition','Федеральный портал Германии для квалифицированных работников','en'),
  ('ES','Миграция — Министерство инклюзии Испании','https://www.inclusion.gob.es/web/migraciones','Министерство инклюзии, социального обеспечения и миграции Испании','es'),
  ('VN','Электронная виза — Национальный портал иммиграции Вьетнама','https://immigration.gov.vn/en_US/web/guest/trang-chu-ttdt','Департамент иммиграции Вьетнама','en'),
  ('TH','Электронные визы — Thai e-Visa','https://www.thaievisa.go.th/','Министерство иностранных дел Таиланда','en')
ON CONFLICT(source_url) DO UPDATE SET
  destination_code=EXCLUDED.destination_code,
  title=EXCLUDED.title,
  authority=EXCLUDED.authority,
  source_language=EXCLUDED.source_language;

CREATE TABLE IF NOT EXISTS source_snapshots (
  id BIGSERIAL PRIMARY KEY,
  source_id BIGINT NOT NULL REFERENCES migration_sources(id) ON DELETE CASCADE,
  fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  http_status INTEGER NOT NULL,
  content_type TEXT NOT NULL DEFAULT '',
  content_hash TEXT NOT NULL,
  extracted_text TEXT NOT NULL DEFAULT '',
  changed BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX IF NOT EXISTS source_snapshots_latest ON source_snapshots(source_id, fetched_at DESC);
CREATE INDEX IF NOT EXISTS source_snapshots_hash ON source_snapshots(source_id, content_hash);
CREATE TABLE IF NOT EXISTS source_changes (
  id BIGSERIAL PRIMARY KEY,
  source_id BIGINT NOT NULL REFERENCES migration_sources(id) ON DELETE CASCADE,
  old_snapshot_id BIGINT REFERENCES source_snapshots(id),
  new_snapshot_id BIGINT NOT NULL REFERENCES source_snapshots(id),
  status TEXT NOT NULL DEFAULT 'review_required'
    CHECK(status IN ('review_required','approved','ignored')),
  change_summary TEXT NOT NULL DEFAULT '',
  detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  reviewed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS source_changes_queue ON source_changes(status, detected_at DESC);

CREATE TABLE IF NOT EXISTS destination_signals (
  id BIGSERIAL PRIMARY KEY,
  destination_code TEXT NOT NULL REFERENCES migration_destinations(code),
  signal_type TEXT NOT NULL CHECK(signal_type IN (
    'language_community','integration_support','cost_of_living','processing_time',
    'administrative_steps','pathway_access'
  )),
  audience_scope TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL,
  source_type TEXT NOT NULL DEFAULT 'official_statistics'
    CHECK(source_type IN ('official_statistics','government','research','community_directory','other')),
  source_url TEXT NOT NULL,
  checked_at DATE NOT NULL,
  status TEXT NOT NULL DEFAULT 'observed'
    CHECK(status IN ('observed','published','expired')),
  valid_from DATE,
  valid_until DATE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(destination_code,signal_type,source_url)
);
CREATE INDEX IF NOT EXISTS destination_signals_current
  ON destination_signals(destination_code,signal_type,checked_at DESC);

CREATE TABLE IF NOT EXISTS knowledge_documents (
  id BIGSERIAL PRIMARY KEY,
  country TEXT NOT NULL,
  title TEXT NOT NULL,
  source_url TEXT NOT NULL,
  source_type TEXT NOT NULL DEFAULT 'official',
  body TEXT NOT NULL,
  checked_at DATE NOT NULL,
  source_id BIGINT REFERENCES migration_sources(id),
  status TEXT NOT NULL DEFAULT 'review_required'
    CHECK(status IN ('review_required','observed','published','expired')),
  valid_from DATE,
  valid_until DATE,
  content_hash TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(country, source_url)
);
ALTER TABLE knowledge_documents ADD COLUMN IF NOT EXISTS source_id BIGINT REFERENCES migration_sources(id);
ALTER TABLE knowledge_documents ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'review_required';
ALTER TABLE knowledge_documents ADD COLUMN IF NOT EXISTS valid_from DATE;
ALTER TABLE knowledge_documents ADD COLUMN IF NOT EXISTS valid_until DATE;
ALTER TABLE knowledge_documents ADD COLUMN IF NOT EXISTS content_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE knowledge_documents ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE knowledge_documents DROP CONSTRAINT IF EXISTS knowledge_documents_status_check;
ALTER TABLE knowledge_documents ADD CONSTRAINT knowledge_documents_status_check
  CHECK(status IN ('review_required','observed','published','expired'));
CREATE INDEX IF NOT EXISTS knowledge_country_checked ON knowledge_documents(country, checked_at DESC);
CREATE INDEX IF NOT EXISTS knowledge_published_lookup ON knowledge_documents(status, country, checked_at DESC);

CREATE TABLE IF NOT EXISTS collection_jobs (
  id BIGSERIAL PRIMARY KEY,
  destination_code TEXT NOT NULL REFERENCES migration_destinations(code),
  topic TEXT NOT NULL,
  max_documents SMALLINT NOT NULL CHECK (max_documents BETWEEN 1 AND 100),
  requested_by BIGINT NOT NULL,
  status TEXT NOT NULL DEFAULT 'queued'
    CHECK (status IN ('queued','running','completed','failed')),
  discovered_count INTEGER NOT NULL DEFAULT 0,
  new_count INTEGER NOT NULL DEFAULT 0,
  changed_count INTEGER NOT NULL DEFAULT 0,
  unchanged_count INTEGER NOT NULL DEFAULT 0,
  skipped_count INTEGER NOT NULL DEFAULT 0,
  failed_count INTEGER NOT NULL DEFAULT 0,
  error_message TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS collection_jobs_queue ON collection_jobs(status, created_at);

CREATE TABLE IF NOT EXISTS collection_job_documents (
  id BIGSERIAL PRIMARY KEY,
  job_id BIGINT NOT NULL REFERENCES collection_jobs(id) ON DELETE CASCADE,
  source_id BIGINT REFERENCES migration_sources(id) ON DELETE SET NULL,
  document_url TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  outcome TEXT NOT NULL CHECK (outcome IN ('new','changed','unchanged','failed','unsupported','skipped')),
  http_status INTEGER,
  error_text TEXT NOT NULL DEFAULT '',
  checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(job_id, document_url)
);
CREATE INDEX IF NOT EXISTS collection_job_documents_job ON collection_job_documents(job_id, checked_at, id);

CREATE TABLE IF NOT EXISTS knowledge_migrations (
  name TEXT PRIMARY KEY,
  applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
