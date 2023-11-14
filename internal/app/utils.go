package app

import (
	"os"
	"strings"
)

// Check if the error message indicates throttling
func IsThrottlingError(err error) bool {
	if err != nil {
		errorMessage := err.Error()
		return HasString(errorMessage, "SubscriptionRequestsThrottled")
	}
	return false
}

func HasString(s, substr string) bool {
	if strings.Contains(s, substr) {
		return true
	} else {
		return false
	}
}

func OpenLogFile(path string) (*os.File, error) {
	logFile, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	return logFile, nil
}
