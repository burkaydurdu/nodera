package logger

type noopLog struct{}

// Noop discards every entry. For tests where log output is irrelevant.
func Noop() ILog { return noopLog{} }

func (noopLog) Error(error)                   {}
func (noopLog) Errorf(string, ...any)         {}
func (noopLog) ErrorWithFields(error, Fields) {}
func (noopLog) Info(...any)                   {}
func (noopLog) Infof(string, ...any)          {}
func (noopLog) InfoWithFields(any, Fields)    {}
func (noopLog) Warn(...any)                   {}
func (noopLog) Warnf(string, ...any)          {}
func (noopLog) WarnWithFields(any, Fields)    {}
func (noopLog) Fatal(...any)                  {}
func (noopLog) Fatalf(string, ...any)         {}
