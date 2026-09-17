package system

import "runtime"

var lineSep = func() string {
	if runtime.GOOS == "windows" {
		return "\r\n"
	}
	return "\n"
}()

// LineSeparator returns the OS line separator: "\r\n" on Windows and
// "\n" elsewhere. The value is fixed for the lifetime of the process.
func LineSeparator() string {
	return lineSep
}
