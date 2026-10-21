package main

import (
	"embed"
	"fmt"
	"os"
	_ "time/tzdata"

	"catcher/cmd/app"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if err := app.Run(assets); err != nil {
		// 发布版没有控制台时，这个错误会丢失；M4 会补原生错误对话框。
		fmt.Fprintf(os.Stderr, "Catcher 启动失败: %v\n", err)
		os.Exit(1)
	}
}
