package utils

import (
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

var asyncQueueSourceDir string

func init() {
	_, file, _, _ := runtime.Caller(0)
	asyncQueueSourceDir = regexp.MustCompile(`utils.utils\.go`).ReplaceAllString(file, "")
}

// FileWithLineNum return the file name and line number of the current file
func FileWithLineNum() string {
	for i := 2; i < 15; i++ {
		_, file, line, ok := runtime.Caller(i)
		if ok && (!strings.HasPrefix(file, asyncQueueSourceDir) || strings.HasSuffix(file, "_test.go")) {
			return file + ":" + strconv.FormatInt(int64(line), 10)
		}
	}

	return ""
}
