// Package logger implements a wrapper around the logrus package.
// The package adds some default fields and allows settings to be changed through a config file.
// Like adding output to a log file (using a multi-writer)
package logger

import (
	"github.com/sirupsen/logrus"
	"io"
	"os"
	"path/filepath"
	"peterdekok.nl/gotools/config"
	"peterdekok.nl/gotools/trap"
	"sync"
)

// Logger is a wrapper around logrus.FieldLogger
type Logger logrus.FieldLogger

type logWrapper struct {
	log  *logrus.Logger
	fp   *os.File
	l    *logrus.Entry
	mux sync.Mutex
}

// Config for the log package
type Config struct {
	File string
}

var (
	l *logWrapper
	cnf = &Config{}
)

func init() {
	config.Add(&struct{Logger *Config}{Logger: cnf})

	l = &logWrapper{
		log: logrus.New(),
	}

	l.log.SetLevel(logrus.DebugLevel)

	l.log.Debug("Initializing logger")

	l.l = l.log.WithField("cmd", filepath.Base(os.Args[0]))

	// Open logfile if applicable, ignore errors (after logging to STDOUT of course)
	if err := Reload(); err != nil {
		l.l.WithError(err).Error("Error opening log file")
	} else {
		// Cleanly close log file on shutdown
		trap.OnKill(func() {
			if l.fp == nil {
				// Ignore when no file pointer specified
				return
			}

			if err := l.fp.Close(); err != nil {
				l.l.WithError(err).Error("Failed to close log file")
			}
		})

		// Reload the logfile on reload signal (USR1)
		trap.OnReload(func() {
			if err := Reload(); err != nil {
				l.l.WithError(err).Error("Failed to reload log file")
			}
		})
	}

	if hn, err := os.Hostname(); err == nil {
		l.l = l.l.WithField("hostname", hn)
	} else {
		l.l.WithError(err).Error("Could not determine hostname")
	}

	l.l.Debug("Log initialized")
}

// New will return the Logger instance, with or without a package field
func New(pkg string) Logger {
	if pkg != "" {
		return l.l.WithField("pkg", pkg)
	}

	return l.l
}

// Reload will reload the log file if enabled through config.
//
// If the config is also reloaded and there is now no log file in it anymore,
// the log file will be closed and output redirected to only STDOUT once more.
//
// To make sure no logs are missed when opening a new file, it follows the following order:
// - First open the new file
// - Next set the file pointer to the new file
// - Last close the original file
func Reload() error {
	// Protect against concurrent reload signals
	l.mux.Lock()
	defer l.mux.Unlock()

	l.l.Debug("About to (re)load log file")

	if cnf.File == "" {
		l.l.Debug("Not using log file, only stdout")

		l.log.SetOutput(os.Stdout)

		if l.fp != nil {
			l.l.Debug("Closing previous log file")

			if err := l.fp.Close(); err != nil {
				l.l.WithError(err).Error("failed to close previous log file")

				return err
			}

			l.fp = nil
		}

		return nil
	}

	abs, err := filepath.Abs(cnf.File)

	if err != nil {
		l.l.WithError(err).Error("Failed to calculate absolute log file path")

		return err
	} else {
		l.l.WithField("path", abs).Debug("Log file")
	}

	prevFile := l.fp

	newFile, err := os.OpenFile(abs, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		l.l.WithError(err).Error("error opening file")

		return err
	}

	// Unknown if this is the first time writing to the file, so just create some extra space!!
	_, _ = newFile.WriteString("\n\n================\n\n\n")

	l.fp = newFile

	l.log.SetOutput(io.MultiWriter(os.Stdout, l.fp))

	l.l.Debug("Log file (re)loaded")

	if prevFile == nil {
		return nil
	}

	if err := prevFile.Close(); err != nil {
		l.l.WithError(err).Error("failed to close previous log file")

		return err
	}

	return nil
}
