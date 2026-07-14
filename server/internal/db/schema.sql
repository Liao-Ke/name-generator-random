-- 随机名字 API 的 PostgreSQL Schema (up)
-- 数据库设计文档见 docs/db/ 风格; 此处即事实来源.

-- 字库: 每个 CharInfo 一行
CREATE TABLE IF NOT EXISTS chars (
  char            text PRIMARY KEY,
  pinyin          text NOT NULL,
  tone            smallint NOT NULL,
  pinyin_no_tone  text NOT NULL,
  initial         text NOT NULL,
  initial_method  text NOT NULL,
  initial_place   text NOT NULL,
  vowel           text NOT NULL,
  vowel_type      text NOT NULL,
  count           integer NOT NULL DEFAULT 0,
  is_polyphone    boolean NOT NULL DEFAULT false
);

-- 来源元数据
CREATE TABLE IF NOT EXISTS sources (
  id          text PRIMARY KEY,          -- wealth / academic / ...
  label       text NOT NULL,
  priority    integer NOT NULL,
  weight      integer NOT NULL,
  category    text NOT NULL,            -- wealth/academic/modern/historic
  description text NOT NULL
);

-- 候选名. name = 2 字名
CREATE TABLE IF NOT EXISTS candidates (
  id          bigserial PRIMARY KEY,
  name        text NOT NULL,
  first_char  text NOT NULL,
  second_char text NOT NULL,
  source_id   text NOT NULL REFERENCES sources(id),
  frequency   integer NOT NULL DEFAULT 0,
  confidence  real NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_candidates_source      ON candidates (source_id);
CREATE INDEX IF NOT EXISTS idx_candidates_first       ON candidates (first_char);
CREATE INDEX IF NOT EXISTS idx_candidates_second      ON candidates (second_char);
CREATE INDEX IF NOT EXISTS idx_candidates_src_first   ON candidates (source_id, first_char);
CREATE INDEX IF NOT EXISTS idx_candidates_src_second  ON candidates (source_id, second_char);
-- 候选名在来源内唯一, 反映源数据事实, 同时为 name_source_names 的反查提供稳定键
CREATE UNIQUE INDEX IF NOT EXISTS uniq_candidates_src_name ON candidates (source_id, name);

-- 来源人/出处明细, 用于详情卡
CREATE TABLE IF NOT EXISTS name_source_names (
  candidate_id bigint NOT NULL REFERENCES candidates(id) ON DELETE CASCADE,
  source_name  text   NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_name_source_names_cid ON name_source_names (candidate_id);

-- API key, 可吊销
CREATE TABLE IF NOT EXISTS api_keys (
  key         text PRIMARY KEY,
  label       text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  revoked_at  timestamptz                  -- NULL = 有效
);
CREATE INDEX IF NOT EXISTS idx_api_keys_revoked ON api_keys (revoked_at);

-- 百家姓候选单字姓氏池. /api/random 缺省 surname 时从本表随机抽一个.
-- import 时仅入在 chars 表中存在的字, 保证 pickRandomSurname 不需再校验 charDb.
CREATE TABLE IF NOT EXISTS surnames (
  char text PRIMARY KEY REFERENCES chars(char) ON DELETE CASCADE
);