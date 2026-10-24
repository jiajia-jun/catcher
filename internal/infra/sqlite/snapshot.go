package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"

	"catcher/internal/domain/entity"
)

// SaveSnapshot 在一个事务内写入课程与作业
//
// 软删除顺序是「先全部置 active=0，再 upsert 回 active=1」，
// 这样不必拼一个可能超过 SQLite 参数上限的 NOT IN 列表；
// 事务隔离保证读连接看不到中间状态。
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
			deadline_at        = excluded.deadline_at,
			deadline_source    = excluded.deadline_source,
			deadline_raw       = excluded.deadline_raw,
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
