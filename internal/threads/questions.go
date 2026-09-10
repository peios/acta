package threads

import "strings"

// Shape/size validation at the server boundary. The harness separately checks
// these answers against the original, still-pending provider request.
func ValidAnswers(answers map[string][]string) bool {
	if len(answers) == 0 || len(answers) > 16 {
		return false
	}
	total := 0
	for id, values := range answers {
		if id == "" || len(values) == 0 || len(values) > 64 {
			return false
		}
		total += len(id)
		for _, value := range values {
			if strings.TrimSpace(value) == "" {
				return false
			}
			total += len(value)
		}
	}
	return total <= 64*1024
}
