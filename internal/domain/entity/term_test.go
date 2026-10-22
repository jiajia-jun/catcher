package entity

import (
	"testing"
	"time"
)

func TestSemesterStart(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "十月属于当年秋季学期",
			now:  time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC),
			want: time.Date(2026, 9, 1, 0, 0, 0, 0, semesterLocation),
		},
		{
			name: "九月一日当天算新学期",
			now:  time.Date(2026, 9, 1, 0, 0, 0, 0, semesterLocation),
			want: time.Date(2026, 9, 1, 0, 0, 0, 0, semesterLocation),
		},
		{
			name: "八月归上一学年",
			now:  time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
			want: time.Date(2025, 9, 1, 0, 0, 0, 0, semesterLocation),
		},
		{
			name: "一月归上一学年",
			now:  time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
			want: time.Date(2025, 9, 1, 0, 0, 0, 0, semesterLocation),
		},
		{
			// 东八区已经进了 9 月，按 UTC 还停在 8 月
			name: "按东八区划线",
			now:  time.Date(2026, 8, 31, 20, 0, 0, 0, time.UTC),
			want: time.Date(2026, 9, 1, 0, 0, 0, 0, semesterLocation),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SemesterStart(c.now); !got.Equal(c.want) {
				t.Fatalf("SemesterStart(%s) = %s, want %s",
					c.now.Format(time.RFC3339), got.Format(time.RFC3339), c.want.Format(time.RFC3339))
			}
		})
	}
}
