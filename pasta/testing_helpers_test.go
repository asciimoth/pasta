package pasta_test

import (
	"fmt"
	"sync"

	"github.com/asciimoth/pasta/pasta"
)

var (
	_ pasta.LogFactory = &StringLoggerFactory{}
	_ pasta.Logger     = &StringLogger{}
)

type StringLoggerFactory struct {
	mu sync.Mutex

	str string
}

func (s *StringLoggerFactory) WorkspaceLogger() pasta.Logger {
	return &StringLogger{
		parent: s,
		prefix: "workspace",
	}
}

func (s *StringLoggerFactory) NodeLogger(id uint64, class string) pasta.Logger {
	return &StringLogger{
		parent: s,
		prefix: fmt.Sprintf("%d %s", id, class),
	}
}

func (s *StringLoggerFactory) Result() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.str
}

func (s *StringLoggerFactory) push(row string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.str += row + "\n"
}

type StringLogger struct {
	parent *StringLoggerFactory
	prefix string
}

func (s *StringLogger) Debug(args ...any) {
	s.parent.push(s.prefix + "[debug]" + fmt.Sprint(args...))
}

func (s *StringLogger) Debugf(format string, args ...any) {
	s.parent.push(s.prefix + "[debug]" + fmt.Sprintf(format, args...))
}

func (s *StringLogger) Info(args ...any) {
	s.parent.push(s.prefix + "[info]" + fmt.Sprint(args...))
}

func (s *StringLogger) Infof(format string, args ...any) {
	s.parent.push(s.prefix + "[info]" + fmt.Sprintf(format, args...))
}

func (s *StringLogger) Warn(args ...any) {
	s.parent.push(s.prefix + "[warn]" + fmt.Sprint(args...))
}

func (s *StringLogger) Warnf(format string, args ...any) {
	s.parent.push(s.prefix + "[warn]" + fmt.Sprintf(format, args...))
}

func (s *StringLogger) Err(args ...any) {
	s.parent.push(s.prefix + "[err]" + fmt.Sprint(args...))
}

func (s *StringLogger) Errf(format string, args ...any) {
	s.parent.push(s.prefix + "[err]" + fmt.Sprintf(format, args...))
}

func (s *StringLogger) Fatal(args ...any) {
	s.parent.push(s.prefix + "[fatal]" + fmt.Sprint(args...))
	panic("fatal log")
}

func (s *StringLogger) Fatalf(format string, args ...any) {
	s.parent.push(s.prefix + "[fatal]" + fmt.Sprintf(format, args...))
	panic("fatal log")
}
