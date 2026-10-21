-- Catcher v1 数据库结构（SQLite 3）
--
-- 约定：
--   * 所有时间列均为 RFC3339 UTC 文本，例如 2026-10-21T12:00:00Z；
--   * *At 列由 Go 写入，SQLite 不负责时区转换；
--   * secret_blob / credential_blob 是 DPAPI 加密后的不透明字节，禁止存明文；
--   * assignments.active = 0 表示软删除，不物理删除；
--   * 唯一键 (account_id, platform, dedup_key) 是作业去重的唯一依据。
--
-- 连接级 PRAGMA 由代码设置，不写在本文件：
--   PRAGMA journal_mode = WAL;
--   PRAGMA foreign_keys = ON;
--   PRAGMA busy_timeout = 5000;
--   PRAGMA synchronous = NORMAL;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

-- 每个平台一行；v1 单用户、单账号。
CREATE TABLE IF NOT EXISTS accounts (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    platform        TEXT    NOT NULL UNIQUE
                    CHECK (platform IN ('chaoxing', 'yuketang', 'tronclass')),
    display_name    TEXT    NOT NULL DEFAULT '',   -- 脱敏后的标识，如 138****1234
    login_state     TEXT    NOT NULL DEFAULT 'disconnected'
                    CHECK (login_state IN ('disconnected', 'connected', 'needs_login', 'error')),
    credential_blob BLOB,                          -- DPAPI(密码)；可为 NULL
    last_login_at   TEXT,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL
);

-- 每个账号一份会话秘密；整体 DPAPI 加密。
CREATE TABLE IF NOT EXISTS sessions (
    account_id  INTEGER PRIMARY KEY
                REFERENCES accounts(id) ON DELETE CASCADE,
    secret_blob BLOB    NOT NULL,
    expires_at  TEXT,                              -- 可选；未知则为 NULL
    updated_at  TEXT    NOT NULL
);

-- 每次平台刷新的结果。
CREATE TABLE IF NOT EXISTS fetch_runs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id     INTEGER NOT NULL
                   REFERENCES accounts(id) ON DELETE CASCADE,
    platform       TEXT    NOT NULL
                   CHECK (platform IN ('chaoxing', 'yuketang', 'tronclass')),
    started_at     TEXT    NOT NULL,
    finished_at    TEXT,
    status         TEXT    NOT NULL DEFAULT 'running'
                   CHECK (status IN ('running', 'success', 'partial', 'failed', 'canceled')),
    courses_total  INTEGER NOT NULL DEFAULT 0,
    courses_ok     INTEGER NOT NULL DEFAULT 0,
    items_seen     INTEGER NOT NULL DEFAULT 0,
    items_upserted INTEGER NOT NULL DEFAULT 0,
    warning_count  INTEGER NOT NULL DEFAULT 0,
    error_summary  TEXT    NOT NULL DEFAULT ''
);

-- 部分失败 / 警告明细。
CREATE TABLE IF NOT EXISTS fetch_issues (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id     INTEGER NOT NULL
               REFERENCES fetch_runs(id) ON DELETE CASCADE,
    scope      TEXT    NOT NULL
               CHECK (scope IN ('platform', 'course', 'assignment')),
    ref        TEXT    NOT NULL DEFAULT '',   -- courseId / dedup_key / 空
    kind       TEXT    NOT NULL
               CHECK (kind IN ('auth', 'network', 'parse', 'business', 'canceled', 'internal')),
    message    TEXT    NOT NULL DEFAULT '',
    raw_path   TEXT    NOT NULL DEFAULT '',   -- 相对 data/ 的失败响应留底路径
    created_at TEXT    NOT NULL
);

-- 课程。雨课堂用 (external_id, classroom_id) 作为复合身份；
-- 其他平台的 classroom_id 为空串（不要用 NULL，避免 UNIQUE 对 NULL 失效）。
CREATE TABLE IF NOT EXISTS courses (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id     INTEGER NOT NULL
                   REFERENCES accounts(id) ON DELETE CASCADE,
    platform       TEXT    NOT NULL
                   CHECK (platform IN ('chaoxing', 'yuketang', 'tronclass')),
    external_id    TEXT    NOT NULL,             -- 平台课程 id，按原始字面量保存
    classroom_id   TEXT    NOT NULL DEFAULT '',  -- 雨课堂 classroom_id
    name           TEXT    NOT NULL DEFAULT '',
    semester_key   TEXT    NOT NULL DEFAULT '',  -- 雨课堂 classroom_name 前缀 / 学在重邮 semester.code
    active         INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    first_seen_at  TEXT    NOT NULL,
    last_seen_at   TEXT    NOT NULL,
    UNIQUE (account_id, platform, external_id, classroom_id)
);

-- 作业。status 只有三态；phase（open/closed/unknown）由 deadline_at 与当前时间计算，不落库。
CREATE TABLE IF NOT EXISTS assignments (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id        INTEGER NOT NULL
                      REFERENCES accounts(id) ON DELETE CASCADE,
    platform          TEXT    NOT NULL
                      CHECK (platform IN ('chaoxing', 'yuketang', 'tronclass')),
    course_id         INTEGER REFERENCES courses(id) ON DELETE SET NULL,
    course_external_id TEXT   NOT NULL DEFAULT '',
    course_name       TEXT    NOT NULL DEFAULT '',
    external_id       TEXT    NOT NULL DEFAULT '',  -- 平台作业 id；学习通为空
    dedup_key         TEXT    NOT NULL,
    title             TEXT    NOT NULL DEFAULT '',
    status            TEXT    NOT NULL
                      CHECK (status IN ('submitted', 'unsubmitted', 'unknown')),
    deadline_at       TEXT,                          -- RFC3339 UTC；NULL = 未知
    deadline_source   TEXT    NOT NULL DEFAULT 'unknown'
                      CHECK (deadline_source IN (
                          'platform_exact', 'detail_exact',
                          'derived_remaining', 'inferred_year', 'unknown'
                      )),
    deadline_raw      TEXT    NOT NULL DEFAULT '',   -- 平台原始时间文本
    detail_url        TEXT    NOT NULL DEFAULT '',
    description       TEXT    NOT NULL DEFAULT '',   -- v1 不展示；预留
    first_seen_at     TEXT    NOT NULL,
    last_seen_at      TEXT    NOT NULL,
    active            INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    last_run_id       INTEGER REFERENCES fetch_runs(id) ON DELETE SET NULL,
    UNIQUE (account_id, platform, dedup_key)
);

CREATE INDEX IF NOT EXISTS idx_courses_account_active
    ON courses (account_id, platform, active);

CREATE INDEX IF NOT EXISTS idx_assignments_active_deadline
    ON assignments (active, deadline_at);

CREATE INDEX IF NOT EXISTS idx_assignments_account_platform
    ON assignments (account_id, platform, active, status, deadline_at);

CREATE INDEX IF NOT EXISTS idx_assignments_status_deadline
    ON assignments (active, status, deadline_at);

CREATE INDEX IF NOT EXISTS idx_assignments_course
    ON assignments (course_id, active);

CREATE INDEX IF NOT EXISTS idx_fetch_issues_run
    ON fetch_issues (run_id);

CREATE INDEX IF NOT EXISTS idx_fetch_runs_account_started
    ON fetch_runs (account_id, platform, started_at DESC);

-- 初始迁移版本。Go 端执行本文件后写入：
--   INSERT INTO schema_migrations(version, applied_at) VALUES (1, '<UTC now>');
