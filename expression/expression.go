package expression

import (
	"github.com/TIBCOSoftware/flogo-lib/logger"
	"github.com/elgs/gojq"
)

//package logger
var log = logger.GetLogger("triggerhttpnew-expression")

//EvalMashlingExpr evaluates mashling expression
func EvalMashlingExpr(expr string, content string) bool {
	result := false

	log.SetLogLevel(logger.DebugLevel)
	log.Debugf("expression - %v ", expr)
	log.Debugf("content: %v", content)

	parser, err := gojq.NewStringQuery(content)
	if err != nil {
		log.Errorf("error while parsing content - %v", err)
		return false
	}

	id, err := parser.Query("id")
	log.Debugf("parser.Query('id'): %v", id)

	// if strings.Contains(content, expr) {
	// 	result = true
	// }

	return result
}
