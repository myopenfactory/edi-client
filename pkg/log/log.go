package log

import (
	"os"

	"github.com/sirupsen/logrus"

	"github.com/myopenfactory/client/pkg/log/eventlog"
	"github.com/myopenfactory/client/pkg/log/filesystem"
	"github.com/myopenfactory/client/pkg/log/mail"
	"github.com/myopenfactory/client/pkg/log/syslog"
)

type Logger struct {
	*logrus.Logger
}

type Option func(*Logger)

func New(opts []Option) *Logger {
	l := logrus.New()
	l.SetFormatter(&logrus.TextFormatter{
		DisableColors: true,
		FullTimestamp: true,
	})

	logger := &Logger{Logger: l}
	for _, opt := range opts {
		opt(logger)
	}
	return logger
}

func (l *Logger) SystemErr(err error) {
	e := &entry{Entry: logrus.NewEntry(l.Logger)}
	e.SystemErr(err)
}

func (l *Logger) WithFields(fields map[string]interface{}) Entry {
	e := l.Logger.WithFields(fields)

	return &entry{e}
}

func WithLevel(level string) Option {
	return func(logger *Logger) {
		lvl, err := logrus.ParseLevel(level)
		if err != nil {
			logger.Printf("failed parsing log level: %s", level)
			os.Exit(1)
		}
		logger.SetLevel(lvl)
	}
}

func WithSyslog(address string) Option {
	return func(logger *Logger) {
		hook, err := syslog.New(address)
		if err != nil {
			logger.Errorf("failed to initialize syslog: %v", address)
			os.Exit(1)
		}
		logger.Logger.AddHook(hook)
	}
}

func WithFilesystem(path string) Option {
	return func(logger *Logger) {
		hook, err := filesystem.New(path)
		if err != nil {
			logger.Errorf("failed to initalize filesystem: %v", err)
			os.Exit(1)
		}
		logger.Logger.AddHook(hook)
	}
}

func WithMail(appname, address, sender, receiver, username, password string) Option {
	return func(logger *Logger) {
		hook := mail.New(appname, address, sender, receiver, username, password)
		logger.Logger.AddHook(hook)
	}
}

func WithEventlog(name string) Option {
	return func(logger *Logger) {
		hook, err := eventlog.New(name)
		if err != nil {
			logger.Errorf("failed to initialize eventlog: %q: %v", name, err)
			os.Exit(1)
		}
		logger.Logger.AddHook(hook)
	}
}
