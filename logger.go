package main

import (
	"fmt"
	"log"
	"os"
)

var flags = log.Flags() | log.LUTC | log.Lmicroseconds | log.Lshortfile
var stdLogger = log.New(os.Stdout, "", flags)
var errLogger = log.New(os.Stderr, "Error: ", flags)

// Log logs a standard message
func Log(format string, a ...interface{}) {
	logMessageTo(stdLogger, format, a...)
}

// LogErrFormat logs an error message
func LogErrFormat(format string, a ...interface{}) {
	logMessageTo(errLogger, format, a...)
}

// LogError logs an error type
func LogError(err error) {
	logMessageTo(errLogger, fmt.Sprintf("%v", err))
}

func logMessageTo(out *log.Logger, format string, a ...interface{}) {
	output := fmt.Sprintf(format, a...)
	out.Output(3, output)
}
