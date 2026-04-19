package state

import (
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func BenchmarkStorePodChurnAndVisibleWindow(b *testing.B) {
	store := NewStore()
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	query := "pod-09"

	for idx := 0; idx < 10000; idx++ {
		pod := newTestPod(fmt.Sprintf("pod-%05d", idx), fmt.Sprintf("ns-%02d", idx%50), corev1.PodRunning, 2, 2, fmt.Sprintf("node-%02d", idx%20), now.Add(-time.Duration(idx)*time.Second), fmt.Sprintf("rv-%d", idx))
		store.UpsertPod("dev", pod)
	}
	b.ResetTimer()

	for idx := 0; idx < b.N; idx++ {
		pod := newTestPod(fmt.Sprintf("pod-%05d", idx%10000), fmt.Sprintf("ns-%02d", idx%50), corev1.PodRunning, 2, 2, fmt.Sprintf("node-%02d", idx%20), now.Add(-time.Duration(idx)*time.Second), fmt.Sprintf("rv-%d", idx+10000))
		store.UpsertPod("dev", pod)

		filtered := 0
		store.ForEachPod(func(row PodRow) bool {
			if strings.Contains(row.SearchText(), query) {
				filtered++
			}
			return true
		})

		window := make([]PodRow, 0, 50)
		skipped := 0
		store.ForEachPod(func(row PodRow) bool {
			if !strings.Contains(row.SearchText(), query) {
				return true
			}
			if skipped < 100 {
				skipped++
				return true
			}
			if len(window) >= 50 {
				return false
			}
			window = append(window, row.WithAge(now))
			return true
		})

		if filtered == 0 || len(window) == 0 {
			b.Fatalf("expected filtered window")
		}
	}
}
