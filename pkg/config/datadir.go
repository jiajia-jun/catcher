package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DataDirOptions 允许测试注入环境。零值字段会使用真实实现。
type DataDirOptions struct {
	Args       []string
	Getenv     func(string) string
	Executable func() (string, error)
	WorkingDir func() (string, error)
	// DevMode 为 true 且未显式指定目录时，使用当前工作目录下的 data/。
	// wails dev 的可执行文件在临时目录，必须靠这个分支避免写进临时目录。
	DevMode bool
}

// DefaultDataDirOptions 返回生产环境使用的选项。
func DefaultDataDirOptions() DataDirOptions {
	return DataDirOptions{
		Args:       os.Args[1:],
		Getenv:     os.Getenv,
		Executable: os.Executable,
		WorkingDir: os.Getwd,
		DevMode:    os.Getenv("devserver") != "",
	}
}

// ResolveDataDir 按以下顺序解析数据目录：
//  1. 命令行 --data-dir <path> / --data-dir=<path>
//  2. 环境变量 CATCHER_DATA_DIR
//  3. wails dev 时的当前工作目录/data
//  4. exe 同级 data/
//
// 解析后会创建目录并做一次写入探测。失败时显式报错，不回退到 C 盘或临时目录。
func ResolveDataDir(opts DataDirOptions) (string, error) {
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.Executable == nil {
		opts.Executable = os.Executable
	}
	if opts.WorkingDir == nil {
		opts.WorkingDir = os.Getwd
	}

	dir, err := dataDirFromArgs(opts.Args)
	if err != nil {
		return "", err
	}
	if dir == "" {
		dir = strings.TrimSpace(opts.Getenv("CATCHER_DATA_DIR"))
	}
	if dir == "" && opts.DevMode {
		cwd, err := opts.WorkingDir()
		if err != nil {
			return "", fmt.Errorf("获取当前工作目录失败: %w", err)
		}
		dir = filepath.Join(cwd, "data")
	}
	if dir == "" {
		exe, err := opts.Executable()
		if err != nil {
			return "", fmt.Errorf("获取可执行文件路径失败: %w", err)
		}
		dir = filepath.Join(filepath.Dir(exe), "data")
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("解析数据目录路径失败: %w", err)
	}
	if err := ensureWritableDir(abs); err != nil {
		return "", err
	}
	return abs, nil
}

func dataDirFromArgs(args []string) (string, error) {
	fs := flag.NewFlagSet("catcher", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("data-dir", "", "数据目录")
	if err := fs.Parse(args); err != nil {
		return "", fmt.Errorf("解析命令行参数失败: %w", err)
	}
	if fs.NArg() > 0 {
		return "", fmt.Errorf("无法识别的参数: %s", strings.Join(fs.Args(), " "))
	}
	return strings.TrimSpace(*dir), nil
}

func ensureWritableDir(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("创建数据目录 %s 失败（请移动 exe 或设置 CATCHER_DATA_DIR）: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".catcher-write-test-*")
	if err != nil {
		return fmt.Errorf("数据目录 %s 不可写（请移动 exe 或设置 CATCHER_DATA_DIR）: %w", dir, err)
	}
	name := f.Name()
	closeErr := f.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return fmt.Errorf("数据目录 %s 写入探测失败: %w", dir, closeErr)
	}
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return fmt.Errorf("数据目录 %s 无法删除探测文件: %w", dir, removeErr)
	}
	return nil
}
