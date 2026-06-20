package enrich

import (
	"regexp"
)

// Redact applies the configured list of regex patterns to the log content,
// replacing all matches with "[REDACTED]".
func Redact(logContent string, patterns []string) string {
	if len(patterns) == 0 {
		return logContent
	}

	result := logContent
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			// Skip invalid patterns to be safe, although they are validated at boot.
			continue
		}
		result = re.ReplaceAllString(result, "[REDACTED]")
	}
	return result
}
