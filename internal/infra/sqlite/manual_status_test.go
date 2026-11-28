package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"catcher/internal/domain/entity"
)

// 已下架的作业不能被手动标记，命令行据此返回 NOT_FOUND
func TestSetManualStatusRejectsInactive(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewRepo(db)
	account := mustYuketangAccount(t, repo)

	at := time.Date(2026, 11, 28, 8, 0, 0, 0, time.UTC)
	deadline := time.Date(2026, 12, 1, 15, 59, 0, 0, time.UTC)
	in := entity.PersistInput{
		AccountID: account.ID,
		Platform:  entity.PlatformYuketang,
		At:        at,
		Result: entity.FetchResult{
			Complete:    true,
			Courses:     []entity.Course{course("1001", "2001")},
			Assignments: []entity.Assignment{yuketangAssignment("1001", "2001", "3001", "下架的作业", &deadline)},
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
		t.Fatalf("初始作业 = %d", len(items))
	}
	id := items[0].ID

	// 平台本次没有返回该作业，Complete=true 触发软删除
	in.At = at.Add(time.Hour)
	in.Result.Assignments = nil
	if _, err := repo.SaveSnapshot(ctx, in); err != nil {
		t.Fatal(err)
	}

	submitted := entity.StatusSubmitted
	if _, err := repo.SetManualStatus(ctx, id, &submitted, at); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("已下架作业应返回 ErrNotFound，实际 %v", err)
	}
}
