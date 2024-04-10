// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"io"

	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/log"
	"golang.org/x/exp/slog"
)

const (
	errorKey   = "LOG15_ERROR"
	timeFormat = "2006-01-02T15:04:05-0700"
)

type CorethLogger struct {
	log.Logger

	logLevel *slog.LevelVar
}

// InitLogger initializes logger with alias and sets the log level and format with the original [os.StdErr] interface
// along with the context logger.
func InitLogger(alias string, level string, jsonFormat bool, writer io.Writer) (CorethLogger, error) {
	logLevel := &slog.LevelVar{}

	var handler slog.Handler
	if jsonFormat {
		handler = &withLevel{
			Handler: log.JSONHandler(writer),
			level:   logLevel,
		}
	} else {
		useColor := false
		handler = &withLevel{
			Handler: log.NewTerminalHandler(writer, useColor),
			level:   logLevel,
		}
	}

	// Create handler
	c := CorethLogger{
		Logger:   log.NewLogger(handler),
		logLevel: logLevel,
	}

	if err := c.SetLogLevel(level); err != nil {
		return CorethLogger{}, err
	}
	return c, nil
}

// SetLogLevel sets the log level of initialized log handler.
func (c *CorethLogger) SetLogLevel(level string) error {
	// Set log level
	logLevel, err := utils.LvlFromString(level)
	if err != nil {
		return err
	}
	c.logLevel.Set(logLevel)
	return nil
}

type withLevel struct {
	slog.Handler
	level slog.Leveler
}

func (h *withLevel) Enabled(ctx context.Context, level slog.Level) bool {
	return h.level.Level() >= level
}
