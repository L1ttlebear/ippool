package main

import (
	"log/slog"

	"github.com/L1ttlebear/ippool/cmd"
	"github.com/L1ttlebear/ippool/utils"
	logutil "github.com/L1ttlebear/ippool/utils/log"
)

func main() {
	if utils.VersionHash == "unknown" {
		logutil.SetupGlobalLogger(slog.LevelDebug)
	} else {
		logutil.SetupGlobalLogger(slog.LevelInfo)
	}
	cmd.Execute()
}
