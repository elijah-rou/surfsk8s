package app

import (
	"path"
	"strings"

	"github.com/elijahrou/surfsk8s/internal/ui/components"
)

type searchMode uint8

const (
	searchModeFuzzy searchMode = iota
	searchModeWildcard
	searchModeExact
)

func detectSearchMode(query string) searchMode {
	query = strings.TrimSpace(query)
	switch {
	case strings.HasPrefix(query, "="):
		return searchModeExact
	case strings.ContainsAny(query, "*?"):
		return searchModeWildcard
	default:
		return searchModeFuzzy
	}
}

func searchQueryValue(query string) string {
	query = strings.TrimSpace(strings.ToLower(query))
	if strings.HasPrefix(query, "=") {
		return strings.TrimSpace(strings.TrimPrefix(query, "="))
	}
	return query
}

func scoreSearchCandidateLower(candidateLower string, query string) (int, bool) {
	queryValue := searchQueryValue(query)
	if queryValue == "" {
		return 0, true
	}

	switch detectSearchMode(query) {
	case searchModeExact:
		if strings.Contains(candidateLower, queryValue) {
			return len(queryValue), true
		}
		return 0, false
	case searchModeWildcard:
		matched, err := path.Match(queryValue, candidateLower)
		if err != nil {
			return 0, false
		}
		if matched {
			return len(queryValue), true
		}
		if !strings.HasPrefix(queryValue, "*") {
			matched, err = path.Match("*"+queryValue, candidateLower)
			if err == nil && matched {
				return len(queryValue), true
			}
		}
		if !strings.HasSuffix(queryValue, "*") {
			matched, err = path.Match(queryValue+"*", candidateLower)
			if err == nil && matched {
				return len(queryValue), true
			}
		}
		matched, err = path.Match("*"+queryValue+"*", candidateLower)
		if err == nil && matched {
			return len(queryValue), true
		}
		return 0, false
	default:
		return components.FuzzyScore(candidateLower, queryValue)
	}
}

func scoreSearchCandidate(candidate string, query string) (int, bool) {
	return scoreSearchCandidateLower(strings.ToLower(candidate), query)
}
