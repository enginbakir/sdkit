package hook

import (
	"os"

	"github.com/wecii/sdkit/internal/report"
)

func init() {
	Register(SessionStart, nil, "value-report-start", valueReport)
	Register("SessionEnd", nil, "value-report", valueReport)
}

// valueReport is the session level of the Value Report. Off unless VALUE_REPORT=on. It never blocks and never prints on stdout (a
// SessionStart hook's stdout would enter the model context): the report goes to stderr and any problem is a silent allow.
//
//	SessionEnd   a true end-of-session signal: mark the session ended and store its summary (reason from the host)
//	SessionStart earlier sessions that left records but never got an end signal get a summary marked "interrupted"
func valueReport(in *Input) Decision {
	if setting(in, "VALUE_REPORT") != "on" {
		return Decision{}
	}
	e := report.New(in.Root)
	e.Err = os.Stderr
	session := in.Str("session_id")
	switch in.Event {
	case "SessionEnd":
		e.SessionEnd(session, in.Str("reason"), false, false)
	case SessionStart:
		e.Recover(session, false, false)
	}
	return Decision{}
}
