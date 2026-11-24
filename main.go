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
		// 发布版无控制台，启动错误只写入 stderr
		fmt.Fprintf(os.Stderr, "Catcher 启动失败: %v\n", err)
		os.Exit(1)
	}
}
