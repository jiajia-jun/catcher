package entity

import "time"

// semesterLocation 固定东八区，与系统时区无关
var semesterLocation = time.FixedZone("CST", 8*3600)

// SemesterStart 返回 now 所属学期的起点
//
// 学习通按自然学年划线：秋季学期从 9 月开始，
// 9 月及以后归当年，1—8 月归上一年
func SemesterStart(now time.Time) time.Time {
	local := now.In(semesterLocation)
	year := local.Year()
	if local.Month() < time.September {
		year--
	}
	return time.Date(year, time.September, 1, 0, 0, 0, 0, semesterLocation)
}
