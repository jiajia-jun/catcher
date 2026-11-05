package chaoxing

import (
	"fmt"
	"strings"
)

// courseMiddlePath 是课程主页的入口，服务器会 302 到该班次的课程页
const courseMiddlePath = "/visit/stucoursemiddle"

// courseWorkTab 是课程页「作业」标签页的 pageHeader 值。
// 实测对照：0 是任务列表，8 是作业列表
const courseWorkTab = "8"

// courseHomeURL 拼课程主页地址，用作作业的跳转链接
//
// 学习通拿不到可以直接提交的作业地址：手机版详情页 task-work 只能看不能交，
// 桌面版 dowork 需要的 standardEnc 由服务端下发。所以统一落到课程页的作业列表，
// 由用户从列表进作业
func (c *Client) courseHomeURL(course courseEntry) string {
	courseID := strings.TrimSpace(course.ID.String())
	classID := strings.TrimSpace(course.ClassID)
	cpi := strings.TrimSpace(course.CPI)
	// 缺任意一个都组不出有效地址，宁可保留手机版详情链接
	if courseID == "" || classID == "" || cpi == "" {
		return ""
	}
	return fmt.Sprintf("%s%s?courseid=%s&clazzid=%s&cpi=%s&ismooc2=1&pageHeader=%s&v=2",
		c.desktopBaseURL, courseMiddlePath, courseID, classID, cpi, courseWorkTab)
}
