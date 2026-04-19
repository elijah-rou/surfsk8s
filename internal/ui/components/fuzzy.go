package components

import "strings"

func FuzzyScore(candidate string, query string) (int, bool) {
	candidate = strings.ToLower(candidate)
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 0, true
	}

	candidateRunes := []rune(candidate)
	queryRunes := []rune(query)
	if len(queryRunes) > len(candidateRunes) {
		return 0, false
	}

	score := 0
	lastMatch := -1
	streak := 0
	for _, queryRune := range queryRunes {
		matched := false
		for idx := lastMatch + 1; idx < len(candidateRunes); idx++ {
			if candidateRunes[idx] != queryRune {
				continue
			}

			score += 10
			if idx == lastMatch+1 {
				streak++
				score += 10 * streak
			} else {
				streak = 0
				score -= idx - lastMatch - 1
			}
			if idx == 0 || isBoundary(candidateRunes[idx-1]) {
				score += 8
			}

			lastMatch = idx
			matched = true
			break
		}
		if !matched {
			return 0, false
		}
	}

	score -= len(candidateRunes) - len(queryRunes)
	return score, true
}

func isBoundary(r rune) bool {
	switch r {
	case '/', '-', '_', ' ', '.':
		return true
	default:
		return false
	}
}
