package logger

import (
	"github.com/piggystats/rerror"
	"github.com/sirupsen/logrus"

	"github.com/piggystats/nodera/utils/environment"
)

//go:generate mockgen -destination=../mock/logger/logger.go -package=mocklogger github.com/piggystats/nodera/logger ILog
type ILog interface {
	Error(error)
	Errorf(format string, args ...any)
	ErrorWithFields(err error, fields Fields)
	Info(info ...any)
	Infof(format string, args ...any)
	InfoWithFields(info any, fields Fields)
	Warn(warn ...any)
	Warnf(format string, args ...any)
	WarnWithFields(warn any, fields Fields)
	Fatal(fatal ...any)
	Fatalf(format string, args ...any)
}

type Log struct {
	logrus *logrus.Logger
}

func New(env string) ILog {
	l := logrus.New()
	if env == environment.Dev {
		l.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	} else {
		l.SetFormatter(&logrus.JSONFormatter{})
	}

	return &Log{logrus: l}
}

func (l *Log) Error(err error) {
	if customError, ok := err.(rerror.Error); ok {
		l.logrus.WithFields(customError.GetFields()).Error(err)

		return
	}

	l.logrus.Error(err)
}

func (l *Log) Errorf(format string, args ...any) {
	l.logrus.Errorf(format, args...)
}

func (l *Log) ErrorWithFields(err error, fields Fields) {
	if customError, ok := err.(rerror.Error); ok {
		l.logrus.WithFields(logrus.Fields{"metadata": customError.GetFields()}).Error(err)

		return
	}

	l.logrus.WithFields(logrus.Fields{"metadata": fields}).Error(err)
}

func (l *Log) Info(info ...any) {
	l.logrus.Info(info...)
}

func (l *Log) Infof(format string, args ...any) {
	l.logrus.Infof(format, args...)
}

func (l *Log) InfoWithFields(info any, fields Fields) {
	l.logrus.WithFields(logrus.Fields(fields)).Info(info)
}

func (l *Log) Warn(warn ...any) {
	l.logrus.Warn(warn...)
}

func (l *Log) Warnf(format string, args ...any) {
	l.logrus.Warnf(format, args...)
}

func (l *Log) WarnWithFields(warn any, fields Fields) {
	l.logrus.WithFields(logrus.Fields(fields)).Warn(warn)
}

func (l *Log) Fatal(fatal ...any) {
	l.logrus.Fatal(fatal...)
}

func (l *Log) Fatalf(format string, args ...any) {
	l.logrus.Fatalf(format, args...)
}
