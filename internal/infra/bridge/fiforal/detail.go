package fiforal

import (
	"net/url"
	"strings"
)

// detailURL 用 taskId + taskModelType 拼稳定的 H5 详情路由。
// 链接不含 token；浏览器自己未登录 FiF 时会落到扫码页
func (c *Client) detailURL(item taskItem) string {
	query := url.Values{"taskId": {item.taskID()}}
	if modelType := strings.TrimSpace(string(item.TaskModelType)); modelType != "" {
		query.Set("taskModelType", modelType)
	}
	return c.h5BaseURL + "#/pages/teaching/details/details?" + query.Encode()
}
