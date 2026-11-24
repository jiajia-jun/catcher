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

	// 旧记录仍在库里，只是 active=0，且不产生重复行
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

// 截止时间未知不覆盖已存值：学习通已提交作业刷新后平台不再提供截止时间
func TestSaveSnapshotPreservesKnownDeadline(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	account := mustYuketangAccount(t, repo)

	at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	known := at.Add(72 * time.Hour)
	knownSource := entity.DeadlineDetailExact
	knownRaw := "截止时间09月25日22时55分18秒"

	base := entity.PersistInput{
		AccountID: account.ID,
		Platform:  entity.PlatformYuketang,
		At:        at,
		Result: entity.FetchResult{
			Complete: true,
			Courses:  []entity.Course{course("1001", "2001")},
		},
	}

	withKnown := yuketangAssignment("1001", "2001", "3001", "已提交作业", &known)
	withKnown.DeadlineSource = knownSource
	withKnown.DeadlineRaw = knownRaw
	neverKnown := yuketangAssignment("1001", "2001", "3002", "首次就未知", nil)
	neverKnown.DeadlineSource = entity.DeadlineUnknown

	first := base
	first.Result.Assignments = []entity.Assignment{withKnown, neverKnown}
	if _, err := repo.SaveSnapshot(ctx, first); err != nil {
		t.Fatal(err)
	}

	load := func() map[string]entity.Assignment {
		t.Helper()
		items, err := repo.ListAssignments(ctx, entity.AssignmentFilter{ShowSubmitted: true})
		if err != nil {
			t.Fatal(err)
		}
		out := make(map[string]entity.Assignment, len(items))
		for _, item := range items {
			out[item.DedupKey] = item
		}
		return out
	}

	// 第二次：平台不再提供截止时间，三列保持上一次的值
	unknown := yuketangAssignment("1001", "2001", "3001", "已提交作业", nil)
	unknown.DeadlineSource = entity.DeadlineUnknown
	second := base
	second.At = at.Add(24 * time.Hour)
	second.Result.Assignments = []entity.Assignment{unknown, neverKnown}
	if _, err := repo.SaveSnapshot(ctx, second); err != nil {
		t.Fatal(err)
	}

	got := load()[withKnown.DedupKey]
	if got.Deadline == nil || !got.Deadline.Equal(known) {
		t.Fatalf("截止时间被清空: %v", got.Deadline)
	}
	if got.DeadlineSource != knownSource {
		t.Fatalf("截止来源 = %q, want %q", got.DeadlineSource, knownSource)
	}
	if got.DeadlineRaw != knownRaw {
		t.Fatalf("截止原文 = %q, want %q", got.DeadlineRaw, knownRaw)
	}

	// 第三次：拿到新的已知截止时间，三列整体覆盖
	newDeadline := known.Add(48 * time.Hour)
	updated := yuketangAssignment("1001", "2001", "3001", "已提交作业", &newDeadline)
	updated.DeadlineSource = entity.DeadlineDerivedRemaining
	updated.DeadlineRaw = "311小时"
	third := base
	third.At = at.Add(48 * time.Hour)
	third.Result.Assignments = []entity.Assignment{updated, neverKnown}
	if _, err := repo.SaveSnapshot(ctx, third); err != nil {
		t.Fatal(err)
	}

	got = load()[updated.DedupKey]
	if got.Deadline == nil || !got.Deadline.Equal(newDeadline) {
		t.Fatalf("新截止时间未覆盖: %v", got.Deadline)
	}
	if got.DeadlineSource != entity.DeadlineDerivedRemaining || got.DeadlineRaw != "311小时" {
		t.Fatalf("截止来源/原文未更新: %q / %q", got.DeadlineSource, got.DeadlineRaw)
	}

	// 从未读到过截止时间的作业保持未知
	got = load()[neverKnown.DedupKey]
	if got.Deadline != nil {
		t.Fatalf("未知作业不应有截止时间: %v", got.Deadline)
	}
	if got.DeadlineSource != entity.DeadlineUnknown {
		t.Fatalf("未知作业来源 = %q", got.DeadlineSource)
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

// 课程列表完整但部分课程失败时，只清理失败课程以外的旧数据
func TestSaveSnapshotCourseScopedSoftDelete(t *testing.T) {
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
				course("1003", "2003"),
			},
			Assignments: []entity.Assignment{
				yuketangAssignment("1001", "2001", "3001", "课程一作业", &deadline),
				yuketangAssignment("1002", "2002", "3002", "课程二作业", &deadline),
				yuketangAssignment("1003", "2003", "3003", "旧学期作业", &deadline),
			},
		},
	}
	if _, err := repo.SaveSnapshot(ctx, full); err != nil {
		t.Fatal(err)
	}

	// 课程列表完整，课程二作业抓取失败，旧学期课程不再出现
	partial := full
	partial.At = base.Add(time.Hour)
	partial.Result = entity.FetchResult{
		Complete:           false,
		CourseListComplete: true,
		Courses: []entity.Course{
			course("1001", "2001"),
			course("1002", "2002"),
		},
		Assignments: []entity.Assignment{
			yuketangAssignment("1001", "2001", "3001", "课程一作业", &deadline),
		},
		FailedCourses: []string{"1002"},
	}
	if _, err := repo.SaveSnapshot(ctx, partial); err != nil {
		t.Fatal(err)
	}

	items, err := repo.ListAssignments(ctx, entity.AssignmentFilter{ShowSubmitted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("active 作业 = %d, want 2（失败课程豁免）", len(items))
	}
	for _, item := range items {
		if item.CourseExternalID == "1003" {
			t.Fatalf("旧学期作业应被软删除: %#v", item)
		}
	}

	var activeCourses, inactiveCourses int
	if err := db.SQL().GetContext(ctx, &activeCourses,
		`SELECT COUNT(*) FROM courses WHERE active = 1`); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().GetContext(ctx, &inactiveCourses,
		`SELECT COUNT(*) FROM courses WHERE active = 0`); err != nil {
		t.Fatal(err)
	}
	if activeCourses != 2 || inactiveCourses != 1 {
		t.Fatalf("课程 active/inactive = %d/%d, want 2/1", activeCourses, inactiveCourses)
	}
}

// 课程列表本身不完整时跳过全部软删除
func TestSaveSnapshotCourseListIncompleteKeepsEverything(t *testing.T) {
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
			Courses:  []entity.Course{course("1001", "2001"), course("1002", "2002")},
			Assignments: []entity.Assignment{
				yuketangAssignment("1001", "2001", "3001", "课程一作业", &deadline),
				yuketangAssignment("1002", "2002", "3002", "课程二作业", &deadline),
			},
		},
	}
	if _, err := repo.SaveSnapshot(ctx, full); err != nil {
		t.Fatal(err)
	}

	partial := full
	partial.At = base.Add(time.Hour)
	partial.Result = entity.FetchResult{
		Complete:           false,
		CourseListComplete: false,
		Courses:            []entity.Course{course("1001", "2001")},
		Assignments: []entity.Assignment{
			yuketangAssignment("1001", "2001", "3001", "课程一作业", &deadline),
		},
	}
	if _, err := repo.SaveSnapshot(ctx, partial); err != nil {
		t.Fatal(err)
	}

	items, err := repo.ListAssignments(ctx, entity.AssignmentFilter{ShowSubmitted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("active 作业 = %d, want 2", len(items))
	}
	var activeCourses int
	if err := db.SQL().GetContext(ctx, &activeCourses,
		`SELECT COUNT(*) FROM courses WHERE active = 1`); err != nil {
		t.Fatal(err)
	}
	if activeCourses != 2 {
		t.Fatalf("active 课程 = %d, want 2", activeCourses)
	}
}

// 手动状态与平台状态分开存：刷新和软删除保持手动值不变
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

	// 平台再次抓取到 unknown，手动值保持不变
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

	// 重复恢复按成功处理，撤销可能重复调用
	if _, err := repo.SetManualStatus(ctx, id, nil, at.Add(6*time.Hour)); err != nil {
		t.Fatalf("重复恢复平台值失败: %v", err)
	}

	if _, err := repo.SetManualStatus(ctx, 999999, &submitted, at); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("不存在的作业应返回 ErrNotFound: %v", err)
	}
}

func mustChaoxingAccount(t *testing.T, repo *Repo) entity.Account {
	t.Helper()
	ctx := context.Background()
	if err := repo.EnsurePlatforms(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	account, err := repo.GetAccount(ctx, entity.PlatformChaoxing)
	if err != nil {
		t.Fatal(err)
	}
	return *account
}

func chaoxingCourse(courseID string) entity.Course {
	return entity.Course{
		Platform:    entity.PlatformChaoxing,
		ExternalID:  courseID,
		Name:        "离散数学A&B",
		SemesterKey: "2026秋",
		Active:      true,
	}
}

// chaoxingLegacyKey 模拟改用 taskrefId 之前的标题去重键
func chaoxingLegacyKey(courseID, title string) string {
	return "chaoxing:" + courseID + ":" + entity.NormalizeTitle(title)
}

func legacyChaoxingAssignment(courseID, title string, deadline *time.Time, raw string) entity.Assignment {
	return entity.Assignment{
		Platform:         entity.PlatformChaoxing,
		CourseExternalID: courseID,
		CourseName:       "离散数学A&B",
		DedupKey:         chaoxingLegacyKey(courseID, title),
		Title:            title,
		Status:           entity.StatusUnsubmitted,
		Deadline:         deadline,
		DeadlineSource:   entity.DeadlineInferredYear,
		DeadlineRaw:      raw,
	}
}

func chaoxingAssignment(courseID, taskrefID, title string, deadline *time.Time, raw string) entity.Assignment {
	return entity.Assignment{
		Platform:         entity.PlatformChaoxing,
		CourseExternalID: courseID,
		CourseName:       "离散数学A&B",
		ExternalID:       taskrefID,
		DedupKey:         entity.ChaoxingDedupKey(courseID, taskrefID),
		Title:            title,
		Status:           entity.StatusUnsubmitted,
		Deadline:         deadline,
		DeadlineSource:   entity.DeadlineInferredYear,
		DeadlineRaw:      raw,
	}
}

func saveChaoxing(t *testing.T, repo *Repo, account entity.Account, at time.Time,
	courseID string, assignments []entity.Assignment) {
	t.Helper()
	in := entity.PersistInput{
		AccountID: account.ID,
		Platform:  entity.PlatformChaoxing,
		At:        at,
		Result: entity.FetchResult{
			Complete:    true,
			Courses:     []entity.Course{chaoxingCourse(courseID)},
			Assignments: assignments,
		},
	}
	if _, err := repo.SaveSnapshot(context.Background(), in); err != nil {
		t.Fatal(err)
	}
}

func listAllChaoxing(t *testing.T, repo *Repo) []entity.Assignment {
	t.Helper()
	items, err := repo.ListAssignments(context.Background(),
		entity.AssignmentFilter{Platforms: []entity.Platform{entity.PlatformChaoxing}, ShowSubmitted: true})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// 同标题的旧标题键行原地换键，行 id 与手动状态不变
func TestSaveSnapshotRekeysLegacyTitleKey(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	account := mustChaoxingAccount(t, repo)

	courseID := "266504745"
	at := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	deadline := time.Date(2026, 11, 17, 15, 55, 50, 0, time.UTC)
	raw := `aria-label="截止时间11月17日23时55分50秒"`

	saveChaoxing(t, repo, account, at, courseID,
		[]entity.Assignment{legacyChaoxingAssignment(courseID, "期末补交", &deadline, raw)})
	items := listAllChaoxing(t, repo)
	if len(items) != 1 {
		t.Fatalf("初始作业 = %d, want 1", len(items))
	}
	oldID := items[0].ID
	submitted := entity.StatusSubmitted
	if _, err := repo.SetManualStatus(ctx, oldID, &submitted, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	incoming := chaoxingAssignment(courseID, "5550001", "期末补交", &deadline, raw)
	saveChaoxing(t, repo, account, at.Add(24*time.Hour), courseID, []entity.Assignment{incoming})

	items = listAllChaoxing(t, repo)
	if len(items) != 1 {
		t.Fatalf("换键后作业 = %d, want 1（不能新建一行）", len(items))
	}
	got := items[0]
	if got.ID != oldID {
		t.Fatalf("行 id = %d, want %d", got.ID, oldID)
	}
	if got.DedupKey != incoming.DedupKey || got.ExternalID != "5550001" {
		t.Fatalf("键/平台 id = %q / %q", got.DedupKey, got.ExternalID)
	}
	if got.EffectiveStatus() != entity.StatusSubmitted {
		t.Fatalf("手动状态 = %s, want submitted", got.EffectiveStatus())
	}
	if !got.FirstSeenAt.Equal(at) {
		t.Fatalf("first_seen_at = %v, want %v", got.FirstSeenAt, at)
	}
}

// 老师改名后同标题出现两次：有手动标记的旧行按截止时间原文迁到旧作业那条
func TestSaveSnapshotRekeysRenamedLegacyByDeadlineRaw(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	account := mustChaoxingAccount(t, repo)

	courseID := "266504745"
	at := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	oldDeadline := time.Date(2026, 11, 17, 15, 55, 50, 0, time.UTC)
	newDeadline := time.Date(2026, 11, 30, 15, 55, 50, 0, time.UTC)
	oldRaw := `aria-label="截止时间11月17日23时55分50秒"`
	newRaw := `aria-label="截止时间11月30日23时55分50秒"`

	saveChaoxing(t, repo, account, at, courseID,
		[]entity.Assignment{legacyChaoxingAssignment(courseID, "课堂作业6 二元关系", &oldDeadline, oldRaw)})
	items := listAllChaoxing(t, repo)
	if len(items) != 1 {
		t.Fatalf("初始作业 = %d, want 1", len(items))
	}
	oldID := items[0].ID
	submitted := entity.StatusSubmitted
	if _, err := repo.SetManualStatus(ctx, oldID, &submitted, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	title := "离散数学课后作业6 二元关系"
	incomingOld := chaoxingAssignment(courseID, "57939574", title, &oldDeadline, oldRaw)
	incomingNew := chaoxingAssignment(courseID, "58803396", title, &newDeadline, newRaw)
	saveChaoxing(t, repo, account, at.Add(24*time.Hour), courseID,
		[]entity.Assignment{incomingNew, incomingOld})

	items = listAllChaoxing(t, repo)
	if len(items) != 2 {
		t.Fatalf("同标题作业 = %d, want 2", len(items))
	}
	byKey := make(map[string]entity.Assignment, len(items))
	for _, item := range items {
		byKey[item.DedupKey] = item
	}
	oldItem, ok := byKey[incomingOld.DedupKey]
	if !ok {
		t.Fatalf("缺少旧作业 %q: %#v", incomingOld.DedupKey, byKey)
	}
	if oldItem.ID != oldID {
		t.Fatalf("旧作业行 id = %d, want %d（必须原地换键）", oldItem.ID, oldID)
	}
	if oldItem.EffectiveStatus() != entity.StatusSubmitted {
		t.Fatalf("旧作业手动状态 = %s, want submitted", oldItem.EffectiveStatus())
	}
	newItem, ok := byKey[incomingNew.DedupKey]
	if !ok {
		t.Fatalf("缺少新作业 %q: %#v", incomingNew.DedupKey, byKey)
	}
	if newItem.ManualStatus != nil {
		t.Fatalf("新作业不应继承手动状态: %#v", newItem.ManualStatus)
	}
	if newItem.Deadline == nil || !newItem.Deadline.Equal(newDeadline) {
		t.Fatalf("新作业截止 = %v, want %v", newItem.Deadline, newDeadline)
	}
}

// 截止时间原文不同、时刻相差在容差内时也能匹配
func TestSaveSnapshotRekeysLegacyByDeadlineTolerance(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	account := mustChaoxingAccount(t, repo)

	courseID := "266504745"
	at := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	legacyDeadline := time.Date(2026, 11, 17, 15, 55, 50, 0, time.UTC)
	newDeadline := legacyDeadline.Add(5 * time.Minute)

	saveChaoxing(t, repo, account, at, courseID,
		[]entity.Assignment{legacyChaoxingAssignment(courseID, "旧标题", &legacyDeadline, "旧原文")})
	items := listAllChaoxing(t, repo)
	oldID := items[0].ID
	submitted := entity.StatusSubmitted
	if _, err := repo.SetManualStatus(ctx, oldID, &submitted, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	incoming := chaoxingAssignment(courseID, "5550001", "新标题", &newDeadline, "新原文")
	saveChaoxing(t, repo, account, at.Add(24*time.Hour), courseID, []entity.Assignment{incoming})

	items = listAllChaoxing(t, repo)
	if len(items) != 1 {
		t.Fatalf("作业数 = %d, want 1", len(items))
	}
	got := items[0]
	if got.ID != oldID || got.DedupKey != incoming.DedupKey {
		t.Fatalf("未按截止时间容差换键: id=%d key=%q", got.ID, got.DedupKey)
	}
	if got.EffectiveStatus() != entity.StatusSubmitted {
		t.Fatalf("手动状态 = %s, want submitted", got.EffectiveStatus())
	}
}

// 截止时间无法唯一确定时不换键，手动标记不复制给别的作业
func TestSaveSnapshotSkipsAmbiguousRekey(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	account := mustChaoxingAccount(t, repo)

	courseID := "266504745"
	at := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	deadlineA := time.Date(2026, 11, 17, 15, 55, 0, 0, time.UTC)
	deadlineB := deadlineA.Add(2 * time.Minute)
	itemDeadline := deadlineA.Add(time.Minute)

	saveChaoxing(t, repo, account, at, courseID, []entity.Assignment{
		legacyChaoxingAssignment(courseID, "旧作业A", &deadlineA, "原文A"),
		legacyChaoxingAssignment(courseID, "旧作业B", &deadlineB, "原文B"),
	})
	items := listAllChaoxing(t, repo)
	if len(items) != 2 {
		t.Fatalf("初始作业 = %d, want 2", len(items))
	}
	submitted := entity.StatusSubmitted
	for _, item := range items {
		if _, err := repo.SetManualStatus(ctx, item.ID, &submitted, at.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	incoming := chaoxingAssignment(courseID, "5550001", "新作业", &itemDeadline, "新原文")
	saveChaoxing(t, repo, account, at.Add(24*time.Hour), courseID, []entity.Assignment{incoming})

	items = listAllChaoxing(t, repo)
	if len(items) != 1 {
		t.Fatalf("active 作业 = %d, want 1", len(items))
	}
	if items[0].DedupKey != incoming.DedupKey || items[0].ManualStatus != nil {
		t.Fatalf("歧义时不应迁移手动状态: %#v", items[0])
	}

	var legacyKeys int
	if err := db.SQL().GetContext(ctx, &legacyKeys,
		`SELECT COUNT(*) FROM assignments WHERE external_id = '' AND manual_status IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if legacyKeys != 2 {
		t.Fatalf("旧行应保持原样，实际 %d 条", legacyKeys)
	}
}

// 缺 taskrefId 的兜底条目和旧行同键时，旧行留给它原地 upsert
func TestSaveSnapshotKeepsLegacyRowForFallbackKey(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	account := mustChaoxingAccount(t, repo)

	courseID := "266504745"
	at := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	deadline := time.Date(2026, 11, 17, 15, 55, 50, 0, time.UTC)
	raw := `aria-label="截止时间11月17日23时55分50秒"`

	saveChaoxing(t, repo, account, at, courseID,
		[]entity.Assignment{legacyChaoxingAssignment(courseID, "无编号作业", &deadline, raw)})
	items := listAllChaoxing(t, repo)
	oldID := items[0].ID
	submitted := entity.StatusSubmitted
	if _, err := repo.SetManualStatus(ctx, oldID, &submitted, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	fallback := legacyChaoxingAssignment(courseID, "无编号作业", &deadline, raw)
	saveChaoxing(t, repo, account, at.Add(24*time.Hour), courseID, []entity.Assignment{fallback})

	items = listAllChaoxing(t, repo)
	if len(items) != 1 {
		t.Fatalf("作业数 = %d, want 1", len(items))
	}
	got := items[0]
	if got.ID != oldID || got.EffectiveStatus() != entity.StatusSubmitted {
		t.Fatalf("兜底条目应原地更新旧行: %#v", got)
	}
	if got.ExternalID != "" {
		t.Fatalf("external_id = %q, want 空", got.ExternalID)
	}
}

// 新键已经存在时不覆盖，把手动状态补到新行
func TestSaveSnapshotCopiesManualStatusWhenNewKeyExists(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	account := mustChaoxingAccount(t, repo)

	courseID := "266504745"
	at := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	deadline := time.Date(2026, 11, 17, 15, 55, 50, 0, time.UTC)
	raw := `aria-label="截止时间11月17日23时55分50秒"`

	// 先写入新键行
	incoming := chaoxingAssignment(courseID, "5550001", "期末补交", &deadline, raw)
	saveChaoxing(t, repo, account, at, courseID, []entity.Assignment{incoming})
	// 再写入同标题的旧键行并标记手动状态
	saveChaoxing(t, repo, account, at.Add(time.Hour), courseID,
		[]entity.Assignment{legacyChaoxingAssignment(courseID, "期末补交", &deadline, raw)})
	items := listAllChaoxing(t, repo)
	if len(items) != 1 {
		t.Fatalf("第二次刷新后 active 作业 = %d, want 1", len(items))
	}
	legacyID := items[0].ID
	submitted := entity.StatusSubmitted
	if _, err := repo.SetManualStatus(ctx, legacyID, &submitted, at.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	saveChaoxing(t, repo, account, at.Add(3*time.Hour), courseID, []entity.Assignment{incoming})

	items = listAllChaoxing(t, repo)
	if len(items) != 1 {
		t.Fatalf("最终 active 作业 = %d, want 1", len(items))
	}
	got := items[0]
	if got.DedupKey != incoming.DedupKey {
		t.Fatalf("active 键 = %q, want %q", got.DedupKey, incoming.DedupKey)
	}
	if got.EffectiveStatus() != entity.StatusSubmitted {
		t.Fatalf("手动状态未复制到新键行: %s", got.EffectiveStatus())
	}
	var legacyRows int
	if err := db.SQL().GetContext(ctx, &legacyRows,
		`SELECT COUNT(*) FROM assignments WHERE external_id = '' AND dedup_key = ?`,
		chaoxingLegacyKey(courseID, "期末补交")); err != nil {
		t.Fatal(err)
	}
	if legacyRows != 0 {
		t.Fatalf("旧行应标记为已迁移，仍有 %d 条 external_id 为空", legacyRows)
	}

	// 清除手动值后再刷新，旧行不应把手动值复制回来
	restored, err := repo.SetManualStatus(ctx, got.ID, nil, at.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if restored.ManualStatus != nil {
		t.Fatalf("恢复平台值失败: %#v", restored.ManualStatus)
	}
	saveChaoxing(t, repo, account, at.Add(5*time.Hour), courseID, []entity.Assignment{incoming})
	items = listAllChaoxing(t, repo)
	if len(items) != 1 {
		t.Fatalf("最终 active 作业 = %d, want 1", len(items))
	}
	if items[0].ManualStatus != nil {
		t.Fatalf("已清除的手动值被旧行复制回来: %#v", items[0].ManualStatus)
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
