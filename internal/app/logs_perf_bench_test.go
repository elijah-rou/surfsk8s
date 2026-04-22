package app

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elijahrou/surfsk8s/internal/state"
)

func benchmarkLogEntrySeries(count int, start time.Time, jsonEvery int) []logEntry {
	entries := make([]logEntry, 0, count)
	for i := 0; i < count; i++ {
		stamp := start.Add(time.Duration(i) * time.Second)
		message := fmt.Sprintf("plain log line %05d from frontend worker", i)
		if jsonEvery > 0 && i%jsonEvery == 0 {
			message = fmt.Sprintf(`{"level":"info","msg":"hello %05d","count":%d,"service":"frontend"}`, i, i)
		}
		entries = append(entries, logEntry{
			UniqueKey:     fmt.Sprintf("src-%d|%d", i%4, i),
			Timestamp:     stamp,
			HasTimestamp:  true,
			TimestampText: stamp.Format(time.RFC3339),
			SourceKey:     fmt.Sprintf("src-%d", i%4),
			SourceLabel:   fmt.Sprintf("pod-%02d/app", i%4),
			Message:       message,
			Order:         i,
		})
	}
	return entries
}

func benchmarkLogsApp(b *testing.B, entries []logEntry) *App {
	b.Helper()
	manager := newTestManagerForBenchmark(b)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenLogs
	app.logTarget = logTargetDeployment
	app.logRange = logRangeLive
	app.logShowTimestamps = true
	app.width = 180
	app.height = 40
	app.resizeTables()
	app.logEntries = append(app.logEntries[:0], entries...)
	app.logRequestToken = 1
	return app
}

func BenchmarkRenderLogs10000Plain(b *testing.B) {
	app := benchmarkLogsApp(b, benchmarkLogEntrySeries(10000, time.Now().Add(-3*time.Hour), 0))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.renderLogs()
	}
}

func BenchmarkRenderLogs10000FuzzyFilter(b *testing.B) {
	app := benchmarkLogsApp(b, benchmarkLogEntrySeries(10000, time.Now().Add(-3*time.Hour), 0))
	app.logFilterQuery = "front worker"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.renderLogs()
	}
}

func BenchmarkRenderLogs5000JSONWrap(b *testing.B) {
	app := benchmarkLogsApp(b, benchmarkLogEntrySeries(5000, time.Now().Add(-3*time.Hour), 1))
	app.logWrap = true
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.renderLogs()
	}
}

func BenchmarkViewLogs10000Plain(b *testing.B) {
	app := benchmarkLogsApp(b, benchmarkLogEntrySeries(10000, time.Now().Add(-3*time.Hour), 0))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.View()
	}
}

func BenchmarkHandleLogsResultAppend200Into10000(b *testing.B) {
	base := benchmarkLogEntrySeries(10000, time.Now().Add(-4*time.Hour), 0)
	appendBatch := benchmarkLogEntrySeries(200, time.Now().Add(-10*time.Minute), 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app := benchmarkLogsApp(b, base)
		app.logEntrySeen = make(map[string]struct{}, len(base))
		for _, entry := range base {
			app.logEntrySeen[entry.UniqueKey] = struct{}{}
		}
		app.logRequestToken = 7
		msg := logsResultMsg{Token: 7, Replace: false, Cursor: time.Now(), Entries: appendBatch}
		_ = app.handleLogsResult(msg)
	}
}

func BenchmarkLogKeyScrollView10000Plain(b *testing.B) {
	app := benchmarkLogsApp(b, benchmarkLogEntrySeries(10000, time.Now().Add(-3*time.Hour), 0))
	_ = app.View()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.updateLogKeys(msg)
		_ = app.View()
	}
}
