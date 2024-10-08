package controller

import (
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	// ErrorReporterStackSize controls the depth of the LIFO error buffer.
	ErrorReporterStackSize int = 10

	// TrimPrefixSuffixLen contols the number of characters to retain at the prefix and the
	// suffix of long strings.
	TrimPrefixSuffixLen = 120
)

// RateLimit controls the status updater rate-limiter so that the first 3 errors will trigger an
// update per every 2 seconds.
func getDefaultRateLimiter() rate.Sometimes {
	return rate.Sometimes{First: 3, Interval: 2 * time.Second}
}

// errorReporter is the error stack implementatoin
type errorReporter struct {
	errorStack  []error
	ratelimiter rate.Sometimes
	errorChan   chan error
	critical    bool // whether a critical error has been reported
}

func NewErrorReporter(errorChan chan error) *errorReporter {
	return &errorReporter{errorStack: []error{}, ratelimiter: getDefaultRateLimiter(), errorChan: errorChan}
}

func (s *errorReporter) PushError(err error) error {
	return s.Push(err, false)
}

func (s *errorReporter) PushCriticalError(err error) error {
	s.critical = true
	return s.Push(err, true)
}

func (s *errorReporter) Push(err error, critical bool) error {
	// ask a status update if trigger is set
	defer s.ratelimiter.Do(func() {
		if s.errorChan != nil {
			s.errorChan <- err
		}
	})

	if len(s.errorStack) == ErrorReporterStackSize {
		copy(s.errorStack, s.errorStack[1:])
		s.errorStack[len(s.errorStack)-1] = err
		return err
	}
	s.errorStack = append(s.errorStack, err)
	return err
}

func (s *errorReporter) Pop() {
	if s.IsEmpty() {
		return
	}
	s.errorStack = s.errorStack[:len(s.errorStack)-1]
}

func (s *errorReporter) Top() error {
	if s.IsEmpty() {
		return nil
	}
	return s.errorStack[len(s.errorStack)-1]
}

func (s *errorReporter) Size() int {
	return len(s.errorStack)
}

func (s *errorReporter) IsEmpty() bool {
	return len(s.errorStack) == 0
}

func (s *errorReporter) IsCritical() bool {
	return s.critical
}

func (s *errorReporter) Report() []string {
	errs := []string{}
	for _, err := range s.errorStack {
		errs = append(errs, trim(err.Error()))
	}
	return errs
}

func (s *errorReporter) String() string {
	return strings.Join(s.Report(), ",")
}

func trim(s string) string {
	if len(s) <= 2*TrimPrefixSuffixLen+5 {
		return s
	}

	return s[0:TrimPrefixSuffixLen-1] + "[...]" + s[len(s)-TrimPrefixSuffixLen:]
}
