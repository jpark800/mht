package expression

import (
	"strings"

	"github.com/TIBCOSoftware/flogo-lib/logger"
)

//package logger
var log = logger.GetLogger("triggerhttpnew-expression")

//EvalMashlingExpr evaluates mashling expression
func EvalMashlingExpr(expr string, content string) bool {
	result := false

	log.SetLogLevel(logger.DebugLevel)
	log.Debugf("expression - %v ", expr)
	log.Debugf("content: %v", content)

	if !strings.Contains(content, expr) {
		result = true
	}

	return result
}
