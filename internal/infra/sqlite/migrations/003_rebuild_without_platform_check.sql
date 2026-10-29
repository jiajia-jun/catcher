-- catcher:foreign_keys=off
--
-- 003 重建 accounts / fetch_runs / courses / assignments，去掉 platform 列的
-- CHECK 白名单。平台取值改由 Go 侧的 entity.Platform.Valid() 校验：
-- 每加一个平台就要重建一次四张表，代价远大于收益。
--
-- 重建期间必须关闭外键：DROP TABLE 会触发引用方的隐式检查。
-- 执行器识别首行标记后，用独立连接先 PRAGMA foreign_keys=OFF 再执行本文件，
-- 结束后恢复外键并用 PRAGMA foreign_key_check 自检。
--
-- 除 platform 外，其余列定义、约束、唯一键与外键与 001 保持一致；
-- assignments 额外包含 002 增加的 manual_status / manual_status_at。

CREATE TABLE accounts_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    platform        TEXT    NOT NULL UNIQUE,
    display_name    TEXT    NOT NULL DEFAULT '',
    login_state     TEXT    NOT NULL DEFAULT 'disconnected'
                    CHECK (login_state IN ('disconnected', 'connected', 'needs_login', 'error')),
    credential_blob BLOB,
    last_login_at   TEXT,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL
);

INSERT INTO accounts_new (id, platform, display_name, login_state, credential_blob,
                          last_login_at, created_at, updated_at)
SELECT id, platform, display_name, login_state, credential_blob,
       last_login_at, created_at, updated_at
FROM accounts;

DROP TABLE accounts;
ALTER TABLE accounts_new RENAME TO accounts;

CREATE TABLE fetch_runs_new (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id     INTEGER NOT NULL
                   REFERENCES accounts(id) ON DELETE CASCADE,
    platform       TEXT    NOT NULL,
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

INSERT INTO fetch_runs_new (id, account_id, platform, started_at, finished_at, status,
                            courses_total, courses_ok, items_seen, items_upserted,
                            warning_count, error_summary)
SELECT id, account_id, platform, started_at, finished_at, status,
       courses_total, courses_ok, items_seen, items_upserted,
       warning_count, error_summary
FROM fetch_runs;

DROP TABLE fetch_runs;
ALTER TABLE fetch_runs_new RENAME TO fetch_runs;

CREATE TABLE courses_new (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id     INTEGER NOT NULL
                   REFERENCES accounts(id) ON DELETE CASCADE,
    platform       TEXT    NOT NULL,
    external_id    TEXT    NOT NULL,
    classroom_id   TEXT    NOT NULL DEFAULT '',
    name           TEXT    NOT NULL DEFAULT '',
    semester_key   TEXT    NOT NULL DEFAULT '',
    active         INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    first_seen_at  TEXT    NOT NULL,
    last_seen_at   TEXT    NOT NULL,
    UNIQUE (account_id, platform, external_id, classroom_id)
);

INSERT INTO courses_new (id, account_id, platform, external_id, classroom_id, name,
                         semester_key, active, first_seen_at, last_seen_at)
SELECT id, account_id, platform, external_id, classroom_id, name,
       semester_key, active, first_seen_at, last_seen_at
FROM courses;

DROP TABLE courses;
ALTER TABLE courses_new RENAME TO courses;

CREATE TABLE assignments_new (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id        INTEGER NOT NULL
                      REFERENCES accounts(id) ON DELETE CASCADE,
    platform          TEXT    NOT NULL,
    course_id         INTEGER REFERENCES courses(id) ON DELETE SET NULL,
    course_external_id TEXT   NOT NULL DEFAULT '',
    course_name       TEXT    NOT NULL DEFAULT '',
    external_id       TEXT    NOT NULL DEFAULT '',
    dedup_key         TEXT    NOT NULL,
    title             TEXT    NOT NULL DEFAULT '',
    status            TEXT    NOT NULL
                      CHECK (status IN ('submitted', 'unsubmitted', 'unknown')),
    manual_status     TEXT
                      CHECK (manual_status IN ('submitted', 'unsubmitted')),
    manual_status_at  TEXT,
    deadline_at       TEXT,
    deadline_source   TEXT    NOT NULL DEFAULT 'unknown'
                      CHECK (deadline_source IN (
                          'platform_exact', 'detail_exact',
                          'derived_remaining', 'inferred_year', 'unknown'
                      )),
    deadline_raw      TEXT    NOT NULL DEFAULT '',
    detail_url        TEXT    NOT NULL DEFAULT '',
    description       TEXT    NOT NULL DEFAULT '',
    first_seen_at     TEXT    NOT NULL,
    last_seen_at      TEXT    NOT NULL,
    active            INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    last_run_id       INTEGER REFERENCES fetch_runs(id) ON DELETE SET NULL,
    UNIQUE (account_id, platform, dedup_key)
);

INSERT INTO assignments_new (id, account_id, platform, course_id, course_external_id,
                             course_name, external_id, dedup_key, title, status,
                             manual_status, manual_status_at, deadline_at, deadline_source,
                             deadline_raw, detail_url, description, first_seen_at,
                             last_seen_at, active, last_run_id)
SELECT id, account_id, platform, course_id, course_external_id,
       course_name, external_id, dedup_key, title, status,
       manual_status, manual_status_at, deadline_at, deadline_source,
       deadline_raw, detail_url, description, first_seen_at,
       last_seen_at, active, last_run_id
FROM assignments;

DROP TABLE assignments;
ALTER TABLE assignments_new RENAME TO assignments;

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

CREATE INDEX IF NOT EXISTS idx_fetch_runs_account_started
    ON fetch_runs (account_id, platform, started_at DESC);
