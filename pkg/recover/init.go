package recover

import (
	"fmt"
	"runtime/debug"

	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/apperror"
)

// Init recovers a panic on the deferring goroutine and logs it with its stack,
// so a runtime error never unwinds the process to a silent exit 0.
func Init(log logger.ILog) {
	if r := recover(); r != nil {
		log.Error(apperror.ErrorPanic.Copy().
			SetMessage(fmt.Sprintf("%v", r)).
			Add("stack", string(debug.Stack())))
	}
}
