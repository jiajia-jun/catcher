package entity

import "errors"

// ErrNotFound 表示记录不存在。仓储实现必须返回它，方便上层用 errors.Is 判断。
var ErrNotFound = errors.New("记录不存在")
