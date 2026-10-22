package yuketang

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"time"
)

// 学期前缀只挂在 classroom_name 上，course_name 不带学期
var semesterRe = regexp.MustCompile(`^(\d{4})(春|夏|秋)-`)

// semester 是客户端从 classroom_name 推断出的学期
// 这是本地推断，不是平台 API 保证，必须随 warning 写进日志
type semester struct {
	Year   int
	Season string
}

func (s semester) Key() string {
	return fmt.Sprintf("%d%s", s.Year, s.Season)
}

func (s semester) rank() int {
	order := map[string]int{"春": 1, "夏": 2, "秋": 3}
	return s.Year*10 + order[s.Season]
}

func parseSemester(classroomName string) (semester, bool) {
	m := semesterRe.FindStringSubmatch(classroomName)
	if m == nil {
		return semester{}, false
	}
	year, err := strconv.Atoi(m[1])
	if err != nil {
		return semester{}, false
	}
	return semester{Year: year, Season: m[2]}, true
}

// currentSemester 按月份推断当前学期
// 9 月–次年 1 月为秋，2–8 月为春；1 月属于上一年秋
func currentSemester(now time.Time) semester {
	year, month := now.Year(), now.Month()
	switch {
	case month >= time.September:
		return semester{Year: year, Season: "秋"}
	case month == time.January:
		return semester{Year: year - 1, Season: "秋"}
	default:
		return semester{Year: year, Season: "春"}
	}
}

// latestSemester 返回列表中最新的学期前缀
func latestSemester(items []courseItem) (semester, bool) {
	var found []semester
	for _, item := range items {
		if s, ok := parseSemester(item.ClassroomName); ok {
			found = append(found, s)
		}
	}
	if len(found) == 0 {
		return semester{}, false
	}
	sort.Slice(found, func(i, j int) bool { return found[i].rank() < found[j].rank() })
	return found[len(found)-1], true
}
