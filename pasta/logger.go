package pasta

// Logger receives workspace diagnostics. Implementations must be safe for concurrent use.
type Logger interface {
	Debug(args ...any)
	Debugf(format string, args ...any)
	Info(args ...any)
	Infof(format string, args ...any)
	Warn(args ...any)
	Warnf(format string, args ...any)
	Err(args ...any)
	Errf(format string, args ...any)
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

type LogFactory interface {
	WorkspaceLogger() Logger
	NodeLogger(id uint64, class string) Logger
}
