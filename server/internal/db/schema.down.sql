-- 随机名字 API Schema (down / 回滚)
-- 顺序: 先依赖方, 再被依赖方

DROP TABLE IF EXISTS surnames;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS name_source_names;
DROP TABLE IF EXISTS candidates;
DROP TABLE IF EXISTS sources;
DROP TABLE IF EXISTS chars;