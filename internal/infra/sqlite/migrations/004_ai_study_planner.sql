-- 可选 AI 与学习计划；默认关闭，不改动作业、账号和会话表。
CREATE TABLE ai_settings (
 id INTEGER PRIMARY KEY CHECK (id=1),
 settings_json TEXT NOT NULL,
 key_blob BLOB
);
CREATE TABLE ai_runs (
 id TEXT PRIMARY KEY,
 month TEXT NOT NULL,
 state TEXT NOT NULL,
 reserved_tokens INTEGER NOT NULL CHECK (reserved_tokens>=0),
 charged_tokens INTEGER NOT NULL DEFAULT 0 CHECK (charged_tokens>=0),
 created_at TEXT NOT NULL,
 finished_at TEXT
);
CREATE INDEX idx_ai_runs_month ON ai_runs(month);
CREATE TABLE ai_fetch_coverage (
 account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 platform TEXT NOT NULL,
 seen_at TEXT NOT NULL,
 complete INTEGER NOT NULL CHECK (complete IN (0,1)),
 warnings INTEGER NOT NULL,
 PRIMARY KEY(account_id, platform)
);
CREATE TABLE study_plans (
 id TEXT PRIMARY KEY,
 plan_json TEXT NOT NULL,
 created_at TEXT NOT NULL
);
