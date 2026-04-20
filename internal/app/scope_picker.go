package app

import (
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elijahrou/surfsk8s/internal/state"
)

func (a *App) openNamespacePicker() tea.Cmd {
	if !a.screenUsesNamespaceScope() {
		a.statusMessage = "namespace scope unavailable here"
		return nil
	}
	a.scopeReturnScreen = a.screen
	a.scopePickerKind = scopePickerNamespace
	a.scopeQuery = ""
	a.screen = screenScopePicker
	a.inputMode = inputModeScopePicker
	a.filter.SetPrompt("n> ")
	a.filter.SetPlaceholder("namespace")
	a.filter.SetValue(a.scopeQuery)
	a.filter.Activate()
	a.refreshScopePicker()
	return nil
}

func (a *App) openContextScopePicker() tea.Cmd {
	if len(a.manager.ConnectedContextNames()) == 0 {
		a.statusMessage = "no connected contexts"
		return nil
	}
	a.scopeReturnScreen = a.screen
	a.scopePickerKind = scopePickerContext
	a.scopeQuery = ""
	a.screen = screenScopePicker
	a.inputMode = inputModeScopePicker
	a.filter.SetPrompt("c> ")
	a.filter.SetPlaceholder("context")
	a.filter.SetValue(a.scopeQuery)
	a.filter.Activate()
	a.refreshScopePicker()
	return nil
}

func (a *App) screenUsesNamespaceScope() bool {
	switch a.screen {
	case screenCatalog, screenPods, screenPodDetails:
		return true
	case screenResourceList, screenResourceDetails:
		return a.activeResource.Namespaced
	default:
		return false
	}
}

func (a *App) refreshScopePicker() {
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = time.Now()
	options := a.scopePickerOptions()
	a.scopeOptions = options
	visible := fuzzyScopeOptions(options, a.scopeQuery)
	a.visibleScopeOptions = visible
	a.visibleRows = len(visible)
	a.totalRows = len(options)
	title := "NAMESPACE"
	if a.scopePickerKind == scopePickerContext {
		title = "CONTEXT"
	}
	a.setNavTable(title, renderScopeRows(visible))
}

func (a *App) scopePickerOptions() []scopeOption {
	switch a.scopePickerKind {
	case scopePickerContext:
		connected := a.manager.ConnectedContextNames()
		options := make([]scopeOption, 0, len(connected)+1)
		options = append(options, scopeOption{Label: "all connected contexts", Value: ""})
		for _, name := range connected {
			options = append(options, scopeOption{Label: name, Value: name})
		}
		return options
	default:
		namespaces := a.availableNamespacesForScope()
		options := make([]scopeOption, 0, len(namespaces)+1)
		options = append(options, scopeOption{Label: "all namespaces", Value: ""})
		for _, namespace := range namespaces {
			if namespace == "" {
				continue
			}
			options = append(options, scopeOption{Label: namespace, Value: namespace})
		}
		return options
	}
}

func (a *App) scopeBaseScreen() screen {
	if a.screen == screenScopePicker {
		return a.scopeReturnScreen
	}
	return a.screen
}

func (a *App) availableNamespacesForScope() []string {
	namespaces := make([]string, 0, 32)
	seen := make(map[string]struct{}, 32)
	appendNamespace := func(namespace string) {
		if namespace == "" {
			return
		}
		if _, ok := seen[namespace]; ok {
			return
		}
		seen[namespace] = struct{}{}
		namespaces = append(namespaces, namespace)
	}
	switch a.scopeBaseScreen() {
	case screenCatalog:
		a.store.ForEachPod(func(row state.PodRow) bool {
			if !a.contextMatches(row.Cluster) {
				return true
			}
			appendNamespace(row.Namespace)
			return true
		})
		a.store.ForEachDeployment(func(row state.DeploymentRow) bool {
			if !a.contextMatches(row.Cluster) {
				return true
			}
			appendNamespace(row.Namespace)
			return true
		})
		a.store.ForEachService(func(row state.ServiceRow) bool {
			if !a.contextMatches(row.Cluster) {
				return true
			}
			appendNamespace(row.Namespace)
			return true
		})
	case screenPods, screenPodDetails:
		a.store.ForEachPod(func(row state.PodRow) bool {
			if !a.contextMatches(row.Cluster) {
				return true
			}
			appendNamespace(row.Namespace)
			return true
		})
	case screenResourceList, screenResourceDetails:
		switch {
		case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
			a.store.ForEachDeployment(func(row state.DeploymentRow) bool {
				if !a.contextMatches(row.Cluster) {
					return true
				}
				appendNamespace(row.Namespace)
				return true
			})
		case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
			a.store.ForEachService(func(row state.ServiceRow) bool {
				if !a.contextMatches(row.Cluster) {
					return true
				}
				appendNamespace(row.Namespace)
				return true
			})
		default:
			for _, row := range a.genericRows {
				if !a.contextMatches(row.Cluster) {
					continue
				}
				appendNamespace(row.Namespace)
			}
		}
	}
	sort.Strings(namespaces)
	return namespaces
}

func fuzzyScopeOptions(options []scopeOption, query string) []scopeOption {
	if strings.TrimSpace(query) == "" {
		return append([]scopeOption(nil), options...)
	}
	type scoredOption struct {
		option scopeOption
		score  int
	}
	matched := make([]scoredOption, 0, len(options))
	for _, option := range options {
		score, ok := scoreSearchCandidate(option.Label, query)
		if !ok {
			continue
		}
		matched = append(matched, scoredOption{option: option, score: score})
	}
	sort.SliceStable(matched, func(i int, j int) bool { return matched[i].score > matched[j].score })
	result := make([]scopeOption, 0, len(matched))
	for _, item := range matched {
		result = append(result, item.option)
	}
	return result
}

func renderScopeRows(options []scopeOption) [][]string {
	rows := make([][]string, 0, len(options))
	for _, option := range options {
		rows = append(rows, []string{option.Label})
	}
	return rows
}

func (a *App) updateScopePickerPrompt(msg tea.Msg) tea.Cmd {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		switch typed.String() {
		case "esc":
			if strings.TrimSpace(a.scopeQuery) != "" {
				a.scopeQuery = ""
				a.filter.Clear()
				a.refreshScopePicker()
				return nil
			}
			a.inputMode = inputModeSearch
			a.filter.Deactivate()
			a.screen = a.scopeReturnScreen
			a.refreshCurrentScreen(time.Now())
			return nil
		case "j", "down":
			a.navTable.MoveDown(1)
			return nil
		case "k", "up":
			a.navTable.MoveUp(1)
			return nil
		case "g", "home":
			a.navTable.MoveTop()
			return nil
		case "G", "end":
			a.navTable.MoveBottom()
			return nil
		case "enter":
			return a.applySelectedScopeOption()
		}
	}
	cmd := a.filter.Update(msg)
	a.scopeQuery = a.filter.Value()
	a.refreshScopePicker()
	return cmd
}

func (a *App) updateScopePickerKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		a.navTable.MoveDown(1)
	case "k", "up":
		a.navTable.MoveUp(1)
	case "g", "home":
		a.navTable.MoveTop()
	case "G", "end":
		a.navTable.MoveBottom()
	case "esc", "backspace":
		a.inputMode = inputModeSearch
		a.filter.Clear()
		a.filter.Deactivate()
		a.screen = a.scopeReturnScreen
		a.refreshCurrentScreen(time.Now())
	case "enter":
		return a.applySelectedScopeOption()
	}
	return nil
}

func (a *App) applySelectedScopeOption() tea.Cmd {
	index := a.navTable.SelectedIndex()
	if index < 0 || index >= len(a.visibleScopeOptions) {
		a.statusMessage = "selection disappeared"
		return nil
	}
	option := a.visibleScopeOptions[index]
	a.inputMode = inputModeSearch
	a.filter.Deactivate()
	a.filter.Clear()
	a.scopeQuery = ""
	switch a.scopePickerKind {
	case scopePickerContext:
		a.applyContextScope(option.Value)
	default:
		a.applyNamespaceScope(option.Value)
	}
	return nil
}

func (a *App) applyContextScope(value string) {
	a.contextScope = value
	a.namespace = ""
	a.screen = a.scopeReturnScreen
	if a.screen == screenPodDetails {
		a.screen = screenPods
	}
	if a.screen == screenResourceDetails {
		a.screen = screenResourceList
	}
	a.refreshCurrentScreen(time.Now())
}

func (a *App) applyNamespaceScope(value string) {
	a.namespace = value
	a.screen = a.scopeReturnScreen
	if a.screen == screenPodDetails {
		a.screen = screenPods
	}
	if a.screen == screenResourceDetails {
		a.screen = screenResourceList
	}
	a.refreshCurrentScreen(time.Now())
}
