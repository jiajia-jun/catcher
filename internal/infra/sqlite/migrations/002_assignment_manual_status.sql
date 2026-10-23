-- 手动提交状态：用户手写的覆盖值，与平台值分开存
-- manual_status 为 NULL 表示没有手动覆盖；刷新只写 status，不会碰这两列
-- manual_status_at 记录手动标记时间，用于界面提示

ALTER TABLE assignments ADD COLUMN manual_status TEXT
    CHECK (manual_status IN ('submitted', 'unsubmitted'));

ALTER TABLE assignments ADD COLUMN manual_status_at TEXT;
