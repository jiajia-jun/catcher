package cli

type versionData struct {
	Version string `json:"version"`
}

func runVersion(deps Deps, args []string) int {
	fs := newCommandFlagSet("version")
	fs.String("data-dir", "", "数据目录")
	help := fs.Bool("help", false, "输出用法")
	if err := fs.Parse(args); err != nil {
		return UsageError(deps.Stderr, err.Error())
	}
	if fs.NArg() > 0 {
		return UsageError(deps.Stderr, "无法识别的参数: "+fs.Arg(0))
	}
	if *help {
		return printUsage(deps.Stdout, versionUsage)
	}

	version := deps.Service.Version()
	if version == "" {
		version = deps.Version
	}
	return writeData(deps.Stdout, versionData{Version: version})
}
