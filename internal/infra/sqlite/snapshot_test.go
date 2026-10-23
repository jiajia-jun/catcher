package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

func mustYuketangAccount(t *testing.T, repo *Repo) entity.Account {
	t.Helper()
	ctx := context.Background()
	if err := repo.EnsurePlatforms(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	account, err := repo.GetAccount(ctx, entity.PlatformYuketang)
	if err != nil {
		t.Fatal(err)
	}
	return *account
}

func yuketangAssignment(courseID, classroomID, activityID, title string, deadline *time.Time) entity.Assignment {
	return entity.Assignment{
		Platform:         entity.PlatformYuketang,
		CourseExternalID: courseID,
		ClassroomID:      classroomID,
		CourseName:       "某门课程",
		ExternalID:       activityID,
		DedupKey:         entity.YuketangDedupKey(courseID, classroomID, activityID),
		Title:            title,
		Status:           entity.StatusUnknown,
		Deadline:         deadline,
		DeadlineSource:   entity.DeadlinePlatformExact,
	}
}

func course(courseID, classroomID string) entity.Course {
	return entity.Course{
		Platform:    entity.PlatformYuketang,
		ExternalID:  courseID,
		ClassroomID: classroomID,
		Name:        "某门课程",
		SemesterKey: "2026秋",
		Active:      true,
	}
}

func TestSaveSnapshotUpsertAndSoftDelete(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	account := mustYuketangAccount(t, repo)

	t1 := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	deadline := time.Date(2026, 10, 20, 15, 59, 59, 0, time.UTC)

	first := entity.PersistInput{
		AccountID: account.ID,
		Platform:  entity.PlatformYuketang,
		At:        t1,
		Result: entity.FetchResult{
			Complete: true,
			Courses:  []entity.Course{course("1001", "2001")},
			Assignments: []entity.Assignment{
				yuketangAssignment("1001", "2001", "3001", "第一次作业", &deadline),
				yuketangAssignment("1001", "2001", "3002", "第二次作业", &deadline),
			},
		},
	}
	upserted, err := repo.SaveSnapshot(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if upserted != 2 {
		t.Fatalf("首次写入 = %d, want 2", upserted)
	}

	// 第二次：只出现第一条，且标题变化、截止时间变化
	t2 := t1.Add(24 * time.Hour)
	newDeadline := deadline.Add(48 * time.Hour)
	second := first
	second.At = t2
	second.Result.Assignments = []entity.Assignment{
		yuketangAssignment("1001", "2001", "3001", "第一次作业（已改名）", &newDeadline),
	}
	if _, err := repo.SaveSnapshot(ctx, second); err != nil {
		t.Fatal(err)
	}

	items, err := repo.ListAssignments(ctx, entity.AssignmentFilter{ShowSubmitted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("软删除后应只剩 1 条 active，实际 %d", len(items))
	}
	got := items[0]
	if got.Title != "第一次作业（已改名）" {
		t.Fatalf("标题未更新: %s", got.Title)
	}
	if got.Deadline == nil || !got.Deadline.Equal(newDeadline) {
		t.Fatalf("截止时间未更新: %v", got.Deadline)
	}
	if !got.FirstSeenAt.Equal(t1) {
		t.Fatalf("first_seen_at 被覆盖: %v", got.FirstSeenAt)
	}
	if !got.LastSeenAt.Equal(t2) {
		t.Fatalf("last_seen_at = %v, want %v", got.LastSeenAt, t2)
	}

	// 旧记录仍在库里，只是 active=0；且没有重复行
	var total, inactive int
	if err := db.SQL().GetContext(ctx, &total, `SELECT COUNT(*) FROM assignments`); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().GetContext(ctx, &inactive, `SELECT COUNT(*) FROM assignments WHERE active = 0`); err != nil {
		t.Fatal(err)
	}
	if total != 2 || inactive != 1 {
		t.Fatalf("总行数 = %d, inactive = %d, want 2/1", total, inactive)
	}

	// 课程也只保留一行，并关联到作业
	var courseCount int
	if err := db.SQL().GetContext(ctx, &courseCount, `SELECT COUNT(*) FROM courses`); err != nil {
		t.Fatal(err)
	}
	if courseCount != 1 {
		t.Fatalf("课程行数 = %d, want 1", courseCount)
	}
	if got.CourseID == 0 {
		t.Fatal("作业未关联课程 id")
	}
}

func TestSaveSnapshotIncompleteKeepsOldRows(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	account := mustYuketangAccount(t, repo)

	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	deadline := base.Add(72 * time.Hour)
	full := entity.PersistInput{
		AccountID: account.ID,
		Platform:  entity.PlatformYuketang,
		At:        base,
		Result: entity.FetchResult{
			Complete: true,
			Courses: []entity.Course{
				course("1001", "2001"),
				course("1002", "2002"),
			},
			Assignments: []entity.Assignment{
				yuketangAssignment("1001", "2001", "3001", "课程一作业", &deadline),
				yuketangAssignment("1002", "2002", "3002", "课程二作业", &deadline),
			},
		},
	}
	if _, err := repo.SaveSnapshot(ctx, full); err != nil {
		t.Fatal(err)
	}

	// 课程二的请求失败：Complete=false，结果里只有课程一
	partial := full
	partial.At = base.Add(time.Hour)
	partial.Result.Complete = false
	partial.Result.Courses = []entity.Course{course("1001", "2001")}
	partial.Result.Assignments = []entity.Assignment{
		yuketangAssignment("1001", "2001", "3001", "课程一作业", &deadline),
	}
	if _, err := repo.SaveSnapshot(ctx, partial); err != nil {
		t.Fatal(err)
	}

	items, err := repo.ListAssignments(ctx, entity.AssignmentFilter{ShowSubmitted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("部分失败时不应软删除，实际 %d 条", len(items))
	}
}

// 手动状态与平台状态分开存：刷新和软删除都不能冲掉手动值
func TestManualStatusSurvivesRefreshAndFilters(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	account := mustYuketangAccount(t, repo)

	at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	deadline := at.Add(48 * time.Hour)
	in := entity.PersistInput{
		AccountID: account.ID,
		Platform:  entity.PlatformYuketang,
		At:        at,
		Result: entity.FetchResult{
			Complete:    true,
			Courses:     []entity.Course{course("1001", "2001")},
			Assignments: []entity.Assignment{yuketangAssignment("1001", "2001", "3001", "手动作业", &deadline)},
		},
	}
	if _, err := repo.SaveSnapshot(ctx, in); err != nil {
		t.Fatal(err)
	}

	items, err := repo.ListAssignments(ctx, entity.AssignmentFilter{ShowSubmitted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("初始作业 = %d 条, want 1", len(items))
	}
	id := items[0].ID

	submitted := entity.StatusSubmitted
	updated, err := repo.SetManualStatus(ctx, id, &submitted, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != entity.StatusUnknown {
		t.Fatalf("平台状态被改写: %s", updated.Status)
	}
	if updated.EffectiveStatus() != entity.StatusSubmitted {
		t.Fatalf("有效状态 = %s, want submitted", updated.EffectiveStatus())
	}
	if updated.ManualStatusAt == nil || !updated.ManualStatusAt.Equal(at.Add(time.Hour)) {
		t.Fatalf("手动时间 = %v", updated.ManualStatusAt)
	}

	// 默认视图隐藏手动已成已提交的记录，状态筛选也按有效状态
	items, err = repo.ListAssignments(ctx, entity.AssignmentFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("默认视图应隐藏已提交，实际 %d 条", len(items))
	}
	items, err = repo.ListAssignments(ctx, entity.AssignmentFilter{
		Statuses: []entity.SubmissionStatus{entity.StatusSubmitted},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].EffectiveStatus() != entity.StatusSubmitted {
		t.Fatalf("状态筛选未按有效状态: %#v", items)
	}

	// 平台再次抓取到 unknown，手动值必须保留
	in.At = at.Add(2 * time.Hour)
	if _, err := repo.SaveSnapshot(ctx, in); err != nil {
		t.Fatal(err)
	}
	items, err = repo.ListAssignments(ctx, entity.AssignmentFilter{ShowSubmitted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].EffectiveStatus() != entity.StatusSubmitted {
		t.Fatalf("刷新后手动值丢失: %#v", items)
	}
	if items[0].ManualStatusAt == nil || !items[0].ManualStatusAt.Equal(at.Add(time.Hour)) {
		t.Fatalf("刷新改动了手动时间: %v", items[0].ManualStatusAt)
	}

	// 软删除后重新出现，手动值仍在
	withoutAssignment := in
	withoutAssignment.At = at.Add(3 * time.Hour)
	withoutAssignment.Result.Assignments = nil
	if _, err := repo.SaveSnapshot(ctx, withoutAssignment); err != nil {
		t.Fatal(err)
	}
	in.At = at.Add(4 * time.Hour)
	if _, err := repo.SaveSnapshot(ctx, in); err != nil {
		t.Fatal(err)
	}
	items, err = repo.ListAssignments(ctx, entity.AssignmentFilter{ShowSubmitted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].EffectiveStatus() != entity.StatusSubmitted {
		t.Fatalf("软删除恢复后手动值丢失: %#v", items)
	}

	// 清除覆盖后回到平台值
	restored, err := repo.SetManualStatus(ctx, id, nil, at.Add(5*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if restored.EffectiveStatus() != entity.StatusUnknown {
		t.Fatalf("恢复后有效状态 = %s, want unknown", restored.EffectiveStatus())
	}
	if restored.ManualStatus != nil || restored.ManualStatusAt != nil {
		t.Fatalf("恢复后手动字段未清空: %#v", restored)
	}
	items, err = repo.ListAssignments(ctx, entity.AssignmentFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("恢复平台值后应重新显示，实际 %d 条", len(items))
	}

	// 重复恢复不应报错，撤销可能重复调用
	if _, err := repo.SetManualStatus(ctx, id, nil, at.Add(6*time.Hour)); err != nil {
		t.Fatalf("重复恢复平台值失败: %v", err)
	}

	if _, err := repo.SetManualStatus(ctx, 999999, &submitted, at); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("不存在的作业应返回 ErrNotFound: %v", err)
	}
}

func TestSaveSnapshotRejectsBadAssignment(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	account := mustYuketangAccount(t, repo)

	bad := entity.Assignment{Title: "没有去重键"}
	_, err := repo.SaveSnapshot(ctx, entity.PersistInput{
		AccountID: account.ID,
		Platform:  entity.PlatformYuketang,
		At:        time.Now().UTC(),
		Result:    entity.FetchResult{Assignments: []entity.Assignment{bad}},
	})
	if err == nil {
		t.Fatal("缺少去重键时应当报错")
	}
}
