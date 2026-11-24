package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"catcher/internal/domain/entity"
)

// SaveSnapshot 在一个事务内写入课程与作业
//
// 软删除顺序是「先全部置 active=0，再 upsert 回 active=1」，
// 以此避开可能超过 SQLite 参数上限的 NOT IN 列表
// 事务隔离保证读连接看不到中间状态
// 课程列表完整但部分课程作业失败时，失败课程下的旧作业保持 active
func (r *Repo) SaveSnapshot(ctx context.Context, in entity.PersistInput) (int, error) {
	if in.AccountID <= 0 {
		return 0, fmt.Errorf("落库缺少 account_id")
	}
	if !in.Platform.Valid() {
		return 0, fmt.Errorf("落库平台无效: %q", in.Platform)
	}

	upserted := 0
	err := r.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		upserted = 0
		if err := r.rekeyLegacyAssignments(ctx, tx, in); err != nil {
			return err
		}
		switch {
		case in.Result.Complete:
			if _, err := tx.ExecContext(ctx,
				`UPDATE courses SET active = 0 WHERE account_id = ? AND platform = ?`,
				in.AccountID, string(in.Platform)); err != nil {
				return fmt.Errorf("标记旧课程失败: %w", err)
			}
			if _, err := tx.ExecContext(ctx,
				`UPDATE assignments SET active = 0 WHERE account_id = ? AND platform = ?`,
				in.AccountID, string(in.Platform)); err != nil {
				return fmt.Errorf("标记旧作业失败: %w", err)
			}
		case in.Result.CourseListComplete:
			if _, err := tx.ExecContext(ctx,
				`UPDATE courses SET active = 0 WHERE account_id = ? AND platform = ?`,
				in.AccountID, string(in.Platform)); err != nil {
				return fmt.Errorf("标记旧课程失败: %w", err)
			}
			if err := deactivateAssignments(ctx, tx, in); err != nil {
				return err
			}
		}

		courseIDs, err := upsertCourses(ctx, tx, in)
		if err != nil {
			return err
		}

		for _, item := range in.Result.Assignments {
			if err := validateAssignment(item); err != nil {
				return err
			}
			courseID := courseIDs[courseKey(item.CourseExternalID, item.ClassroomID)]
			if err := upsertAssignment(ctx, tx, in, item, courseID); err != nil {
				return err
			}
			upserted++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return upserted, nil
}

func courseKey(externalID, classroomID string) string {
	return externalID + "\x00" + classroomID
}

// deactivateAssignments 清理本次未出现的旧作业，失败课程下的记录豁免
func deactivateAssignments(ctx context.Context, tx *sqlx.Tx, in entity.PersistInput) error {
	failed := make([]string, 0, len(in.Result.FailedCourses))
	seen := make(map[string]struct{}, len(in.Result.FailedCourses))
	for _, externalID := range in.Result.FailedCourses {
		if externalID == "" {
			continue
		}
		if _, ok := seen[externalID]; ok {
			continue
		}
		seen[externalID] = struct{}{}
		failed = append(failed, externalID)
	}

	query := `UPDATE assignments SET active = 0 WHERE account_id = ? AND platform = ?`
	args := []any{in.AccountID, string(in.Platform)}
	if len(failed) > 0 {
		query += ` AND course_external_id NOT IN (?` + strings.Repeat(",?", len(failed)-1) + `)`
		for _, externalID := range failed {
			args = append(args, externalID)
		}
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("标记旧作业失败: %w", err)
	}
	return nil
}

func validateAssignment(item entity.Assignment) error {
	if item.DedupKey == "" {
		return fmt.Errorf("作业 %q 缺少去重键", item.Title)
	}
	if item.Title == "" {
		return fmt.Errorf("作业 %q 缺少标题", item.DedupKey)
	}
	if item.Status != "" && !item.Status.Valid() {
		return fmt.Errorf("作业 %q 状态无效: %q", item.DedupKey, item.Status)
	}
	if item.DeadlineSource != "" && !item.DeadlineSource.Valid() {
		return fmt.Errorf("作业 %q 截止来源无效: %q", item.DedupKey, item.DeadlineSource)
	}
	return nil
}

// upsertCourses 写入课程并返回 (external_id, classroom_id) → 数据库 id 的映射
func upsertCourses(ctx context.Context, tx *sqlx.Tx, in entity.PersistInput) (map[string]int64, error) {
	ids := make(map[string]int64, len(in.Result.Courses))
	for _, course := range in.Result.Courses {
		if course.ExternalID == "" {
			return nil, fmt.Errorf("课程缺少 external_id: %q", course.Name)
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO courses (account_id, platform, external_id, classroom_id, name,
				semester_key, active, first_seen_at, last_seen_at)
			VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)
			ON CONFLICT(account_id, platform, external_id, classroom_id) DO UPDATE SET
				name         = excluded.name,
				semester_key = excluded.semester_key,
				active       = 1,
				last_seen_at = excluded.last_seen_at`,
			in.AccountID, string(in.Platform), course.ExternalID, course.ClassroomID,
			course.Name, course.SemesterKey, formatTime(in.At), formatTime(in.At))
		if err != nil {
			return nil, fmt.Errorf("写入课程 %s 失败: %w", course.ExternalID, err)
		}

		var id int64
		if err := tx.GetContext(ctx, &id, `
			SELECT id FROM courses
			WHERE account_id = ? AND platform = ? AND external_id = ? AND classroom_id = ?`,
			in.AccountID, string(in.Platform), course.ExternalID, course.ClassroomID); err != nil {
			return nil, fmt.Errorf("回读课程 %s 失败: %w", course.ExternalID, err)
		}
		ids[courseKey(course.ExternalID, course.ClassroomID)] = id
	}
	return ids, nil
}

func upsertAssignment(ctx context.Context, tx *sqlx.Tx, in entity.PersistInput,
	item entity.Assignment, courseID int64) error {
	status := item.Status
	if status == "" {
		status = entity.StatusUnknown
	}
	source := item.DeadlineSource
	if source == "" {
		source = entity.DeadlineUnknown
	}

	var courseRef any
	if courseID > 0 {
		courseRef = courseID
	}
	var runRef any
	if in.RunID > 0 {
		runRef = in.RunID
	}
	var deadline any
	if item.Deadline != nil {
		deadline = formatTime(*item.Deadline)
	}

	// 截止时间读不到时保留库中旧值
	// 学习通等平台对已提交作业不再提供截止时间，用 NULL 覆盖会擦掉已知数据
	// 三列以 excluded.deadline_at 为唯一开关，整体保留或整体覆盖
	_, err := tx.ExecContext(ctx, `
		INSERT INTO assignments (account_id, platform, course_id, course_external_id,
			course_name, external_id, dedup_key, title, status, deadline_at,
			deadline_source, deadline_raw, detail_url, description,
			first_seen_at, last_seen_at, active, last_run_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
		ON CONFLICT(account_id, platform, dedup_key) DO UPDATE SET
			course_id          = excluded.course_id,
			course_external_id = excluded.course_external_id,
			course_name        = excluded.course_name,
			external_id        = excluded.external_id,
			title              = excluded.title,
			status             = excluded.status,
			deadline_at        = COALESCE(excluded.deadline_at, assignments.deadline_at),
			deadline_source    = CASE
				WHEN excluded.deadline_at IS NULL AND assignments.deadline_at IS NOT NULL
				THEN assignments.deadline_source
				ELSE excluded.deadline_source
			END,
			deadline_raw       = CASE
				WHEN excluded.deadline_at IS NULL AND assignments.deadline_at IS NOT NULL
				THEN assignments.deadline_raw
				ELSE excluded.deadline_raw
			END,
			detail_url         = excluded.detail_url,
			description        = excluded.description,
			last_seen_at       = excluded.last_seen_at,
			active             = 1,
			last_run_id        = excluded.last_run_id`,
		in.AccountID, string(in.Platform), courseRef, item.CourseExternalID,
		item.CourseName, item.ExternalID, item.DedupKey, item.Title, string(status), deadline,
		string(source), item.DeadlineRaw, item.DetailURL, item.Description,
		formatTime(in.At), formatTime(in.At), runRef)
	if err != nil {
		return fmt.Errorf("写入作业 %s 失败: %w", item.DedupKey, err)
	}
	return nil
}

// legacyRekeyTolerance 是存量作业换键时比较截止时间的容差
// 列表剩余时间精确到分钟，换算出的绝对时刻允许几十秒偏差
const legacyRekeyTolerance = 10 * time.Minute

// legacyAssignmentRow 是仍在使用标题去重键的旧行
type legacyAssignmentRow struct {
	ID             int64          `db:"id"`
	DedupKey       string         `db:"dedup_key"`
	CourseID       string         `db:"course_external_id"`
	Title          string         `db:"title"`
	ManualStatus   sql.NullString `db:"manual_status"`
	ManualStatusAt sql.NullString `db:"manual_status_at"`
	DeadlineRaw    string         `db:"deadline_raw"`
	DeadlineAt     sql.NullString `db:"deadline_at"`
}

// rekeyLegacyAssignments 把仍用标题键的旧行原地换成平台作业 id 键
// 换键保留行 id、手动状态与首次出现时间，后续 upsert 命中的是同一行
// 匹配顺序：本次结果已占用旧键 → 同课程同标题且标题唯一 → 截止时间唯一
// 匹配失败的行保持原样，不把手动标记复制给别的作业
func (r *Repo) rekeyLegacyAssignments(ctx context.Context, tx *sqlx.Tx, in entity.PersistInput) error {
	var rows []legacyAssignmentRow
	if err := tx.SelectContext(ctx, &rows, `
		SELECT id, dedup_key, course_external_id, title, manual_status, manual_status_at,
		       deadline_raw, deadline_at
		FROM assignments
		WHERE account_id = ? AND platform = ? AND external_id = ''
		ORDER BY id`, in.AccountID, string(in.Platform)); err != nil {
		return fmt.Errorf("查询旧去重键作业失败: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	items := in.Result.Assignments
	legacyKeys := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		legacyKeys[row.DedupKey] = struct{}{}
	}
	// 本次结果里已经出现相同旧键时，旧行留给这条作业原地 upsert
	claimed := make(map[string]struct{})
	for _, item := range items {
		if _, ok := legacyKeys[item.DedupKey]; ok {
			claimed[item.DedupKey] = struct{}{}
		}
	}

	byID := make(map[int64]legacyAssignmentRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	matched := make(map[int64]int)
	itemUsed := make([]bool, len(items))

	// 第一轮：同课程同标题，且该标题在本次结果中只有一条
	type courseTitle struct{ course, title string }
	byTitle := make(map[courseTitle][]int)
	for i, item := range items {
		if item.ExternalID == "" {
			continue
		}
		key := courseTitle{item.CourseExternalID, item.Title}
		byTitle[key] = append(byTitle[key], i)
	}
	for _, row := range rows {
		if _, ok := claimed[row.DedupKey]; ok {
			continue
		}
		candidates := byTitle[courseTitle{row.CourseID, row.Title}]
		if len(candidates) != 1 || itemUsed[candidates[0]] {
			continue
		}
		matched[row.ID] = candidates[0]
		itemUsed[candidates[0]] = true
	}

	// 第二轮：只剩带手动标记的旧行，先比截止时间原文
	type courseRaw struct{ course, raw string }
	legacyByRaw := make(map[courseRaw][]int64)
	itemByRaw := make(map[courseRaw][]int)
	for _, row := range rows {
		if !row.ManualStatus.Valid || strings.TrimSpace(row.DeadlineRaw) == "" {
			continue
		}
		if _, ok := claimed[row.DedupKey]; ok {
			continue
		}
		if _, ok := matched[row.ID]; ok {
			continue
		}
		key := courseRaw{row.CourseID, row.DeadlineRaw}
		legacyByRaw[key] = append(legacyByRaw[key], row.ID)
	}
	for i, item := range items {
		if item.ExternalID == "" || itemUsed[i] || strings.TrimSpace(item.DeadlineRaw) == "" {
			continue
		}
		key := courseRaw{item.CourseExternalID, item.DeadlineRaw}
		itemByRaw[key] = append(itemByRaw[key], i)
	}
	for key, ids := range legacyByRaw {
		candidates := itemByRaw[key]
		if len(ids) != 1 || len(candidates) != 1 || itemUsed[candidates[0]] {
			continue
		}
		matched[ids[0]] = candidates[0]
		itemUsed[candidates[0]] = true
	}

	// 第三轮：截止时刻相差不超过容差，且两侧都唯一
	legacyDeadlines := make(map[int64]time.Time)
	for _, row := range rows {
		if !row.ManualStatus.Valid || !row.DeadlineAt.Valid {
			continue
		}
		if _, ok := claimed[row.DedupKey]; ok {
			continue
		}
		if _, ok := matched[row.ID]; ok {
			continue
		}
		deadline, err := parseNullableTime(row.DeadlineAt)
		if err != nil {
			return fmt.Errorf("解析旧作业截止时间失败: %w", err)
		}
		if deadline != nil {
			legacyDeadlines[row.ID] = *deadline
		}
	}
	legacyEdges := make(map[int64][]int)
	itemEdges := make(map[int][]int64)
	for id, legacyDeadline := range legacyDeadlines {
		row := byID[id]
		for i, item := range items {
			if item.ExternalID == "" || itemUsed[i] || item.Deadline == nil {
				continue
			}
			if item.CourseExternalID != row.CourseID {
				continue
			}
			if diff := legacyDeadline.Sub(*item.Deadline); diff <= legacyRekeyTolerance && diff >= -legacyRekeyTolerance {
				legacyEdges[id] = append(legacyEdges[id], i)
				itemEdges[i] = append(itemEdges[i], id)
			}
		}
	}
	for id, candidates := range legacyEdges {
		if len(candidates) != 1 || len(itemEdges[candidates[0]]) != 1 {
			continue
		}
		matched[id] = candidates[0]
		itemUsed[candidates[0]] = true
	}

	// 执行换键；新键已被占用时把手动状态补到新行
	for id, index := range matched {
		row := byID[id]
		item := items[index]
		var existingID int64
		err := tx.GetContext(ctx, &existingID, `
			SELECT id FROM assignments
			WHERE account_id = ? AND platform = ? AND dedup_key = ? AND id <> ?`,
			in.AccountID, string(in.Platform), item.DedupKey, id)
		switch {
		case err == nil:
			if row.ManualStatus.Valid {
				if _, err := tx.ExecContext(ctx, `
					UPDATE assignments SET manual_status = ?, manual_status_at = ?
					WHERE id = ? AND manual_status IS NULL`,
					row.ManualStatus, row.ManualStatusAt, existingID); err != nil {
					return fmt.Errorf("迁移手动状态失败: %w", err)
				}
			}
			// 标记旧行已迁移，避免后续刷新重复匹配并把手动值复制回来
			if _, err := tx.ExecContext(ctx, `
				UPDATE assignments SET external_id = ? WHERE id = ? AND external_id = ''`,
				item.ExternalID, id); err != nil {
				return fmt.Errorf("标记旧作业已迁移失败: %w", err)
			}
			continue
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("检查新去重键失败: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE assignments SET dedup_key = ?, external_id = ?
			WHERE id = ? AND external_id = ''`,
			item.DedupKey, item.ExternalID, id); err != nil {
			return fmt.Errorf("迁移旧去重键失败: %w", err)
		}
		r.logger.Info("作业旧去重键迁移",
			"platform", string(in.Platform), "old", row.DedupKey, "new", item.DedupKey)
	}
	return nil
}
