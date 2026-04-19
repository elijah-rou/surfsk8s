package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	corev1 "k8s.io/api/core/v1"

	"github.com/elijahrou/surfsk8s/internal/actions"
	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/components"
	"github.com/elijahrou/surfsk8s/internal/ui/theme"
	"github.com/elijahrou/surfsk8s/internal/ui/views"
)

type Config struct {
	InitialNamespace string
	KubeconfigPath   string
}

type tickMsg time.Time

type connectResultMsg struct {
	contexts []string
	err      error
}

type screen int

const (
	screenContexts screen = iota
	screenCatalog
	screenGroupResources
	screenPods
	screenResourceList
	screenPodDetails
	screenResourceDetails
	screenCommands
	screenActionPicker
	screenConfirmAction
)

type pickerMode int

const (
	pickerModeEnter pickerMode = iota
	pickerModeAdd
)

type commandItem struct {
	Name        string
	Description string
	Run         func(*App)
}

// App is the top-level Bubbletea model.
// Orchestrates cluster connections, state, and UI views.
type App struct {
	store    *state.Store
	manager  *cluster.Manager
	executor *actions.Executor

	podsView        views.PodsView
	deploymentsView views.DeploymentsView
	servicesView    views.ServicesView
	nodesView       views.NodesView

	navTable      components.Table
	podTable      components.Table
	resourceTable components.Table
	filter        components.Filter
	statusBar     components.StatusBar
	commands      []commandItem
	screen        screen
	prevScreen    screen
	pickerMode    pickerMode

	width  int
	height int

	statusMessage string
	activity      string
	connecting    bool
	inputMode     inputMode

	actionReturnScreen  screen
	pendingAction       pendingAction
	pendingRemotePort   int
	actionPickerTitle   string
	actionPickerFooter  string
	actionPickerOptions []actionOption
	confirmDescription  string
	confirmRun          func() tea.Cmd

	contexts        []cluster.ContextInfo
	visibleContexts []cluster.ContextInfo
	selectedContext map[string]bool
	contextQuery    string

	catalog       []cluster.ResourceGroup
	visibleGroups []cluster.ResourceGroup
	catalogQuery  string

	activeGroup      cluster.ResourceGroup
	visibleResources []cluster.ResourceKind
	resourceQuery    string
	activeResource   cluster.ResourceKind

	namespace          string
	namespaces         []string
	podQuery           string
	resourceQuery2     string
	activePod          state.PodDetails
	activeDeployment   state.DeploymentDetails
	activeService      state.ServiceDetails
	activeNode         state.NodeDetails
	lastDataVersion    uint64
	lastManagerVersion uint64
	lastTick           time.Time
	visibleRows        int
	totalRows          int

	visibleCommands []commandItem
	commandQuery    string

	podSort        listSortState
	deploymentSort listSortState
	serviceSort    listSortState
	nodeSort       listSortState

	sortedPods        []state.PodRow
	sortedDeployments []state.DeploymentRow
	sortedServices    []state.ServiceRow
	sortedNodes       []state.NodeRow
}

func New(store *state.Store, manager *cluster.Manager, cfg Config) *App {
	if store == nil {
		panic("app.New: nil store")
	}
	if manager == nil {
		panic("app.New: nil manager")
	}

	podsView := views.NewPodsView()
	deploymentsView := views.NewDeploymentsView()
	servicesView := views.NewServicesView()
	nodesView := views.NewNodesView()
	navTable := components.NewTable([]components.Column{{Title: "ITEMS", Width: 80}})
	podTable := components.NewTable(podsView.Columns())
	podTable.SetEmptyMessage("No pods")
	resourceTable := components.NewTable(deploymentsView.Columns())
	filter := components.NewFilter()
	statusBar := components.NewStatusBar()
	contexts := manager.AvailableContexts()
	selectedContext := make(map[string]bool, len(contexts))
	for _, context := range contexts {
		if context.Current {
			selectedContext[context.Name] = true
		}
	}

	app := &App{
		namespace:       cfg.InitialNamespace,
		store:           store,
		manager:         manager,
		executor:        actions.NewExecutor(cfg.KubeconfigPath),
		podsView:        podsView,
		deploymentsView: deploymentsView,
		servicesView:    servicesView,
		nodesView:       nodesView,
		navTable:        navTable,
		podTable:        podTable,
		resourceTable:   resourceTable,
		filter:          filter,
		statusBar:       statusBar,
		screen:          screenContexts,
		pickerMode:      pickerModeEnter,
		contexts:        contexts,
		selectedContext: selectedContext,
		namespaces:      []string{""},
	}
	app.commands = []commandItem{
		{Name: "add-context", Description: "Open context picker, connect more kubeconfig contexts", Run: func(a *App) { a.openContextPicker(pickerModeAdd) }},
		{Name: "catalog", Description: "Return to grouped resource catalog", Run: func(a *App) { a.screen = screenCatalog; a.refreshCatalog() }},
		{Name: "pods", Description: "Open pods resource list", Run: func(a *App) {
			a.openResourceList(cluster.ResourceKind{Display: "Pods", Resource: "pods", Namespaced: true})
		}},
		{Name: "deployments", Description: "Open deployments resource list", Run: func(a *App) {
			a.openResourceList(cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true})
		}},
		{Name: "services", Description: "Open services resource list", Run: func(a *App) {
			a.openResourceList(cluster.ResourceKind{Display: "Services", Resource: "services", Namespaced: true})
		}},
		{Name: "nodes", Description: "Open nodes resource list", Run: func(a *App) {
			a.openResourceList(cluster.ResourceKind{Display: "Nodes", Resource: "nodes", Namespaced: false})
		}},
	}
	app.refreshContextRows()
	return app
}

func (a *App) Init() tea.Cmd {
	return tickCmd()
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if a.filter.Active() {
		return a, a.updateFilter(msg)
	}

	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = typed.Width
		a.height = typed.Height
		a.resizeTables()
		a.refreshCurrentScreen(time.Now())
		return a, nil

	case tea.KeyMsg:
		return a, a.updateKey(typed)

	case tickMsg:
		now := time.Time(typed)
		if a.shouldRefresh(now) {
			a.refreshCurrentScreen(now)
		}
		return a, tickCmd()

	case connectResultMsg:
		a.connecting = false
		a.activity = ""
		if typed.err != nil {
			a.statusMessage = typed.err.Error()
			return a, nil
		}
		a.statusMessage = fmt.Sprintf("connected %d context(s)", len(typed.contexts))
		a.screen = screenCatalog
		a.refreshCatalog()
		return a, nil

	case actionResultMsg:
		if typed.err != nil {
			a.statusMessage = typed.description + ": " + typed.err.Error()
		} else {
			a.statusMessage = typed.description + " complete"
		}
		a.activity = ""
		a.refreshCurrentScreen(time.Now())
		return a, nil
	}

	return a, nil
}

func (a *App) View() string {
	title, body, footer := a.currentView()
	inputLabel, inputValue, inputActive := a.statusInputState()
	status := a.statusBar.View(components.StatusBarState{
		Clusters:    a.manager.Statuses(),
		Namespace:   a.currentNamespaceLabel(),
		InputLabel:  inputLabel,
		InputValue:  inputValue,
		InputActive: inputActive,
		VisibleRows: a.visibleRows,
		TotalRows:   a.totalRows,
		Footer:      footer,
		Activity:    a.activity,
	})

	sections := []string{theme.HeaderStyle.Render(title)}
	if a.statusMessage != "" {
		sections = append(sections, theme.StatusWarn.Render(a.statusMessage))
	}
	if a.filter.Active() || strings.TrimSpace(a.currentQuery()) != "" {
		sections = append(sections, a.filter.View())
	}
	sections = append(sections, body, status)
	return lipgloss.NewStyle().Padding(0, 1).Render(strings.Join(sections, "\n"))
}

func (a *App) updateFilter(msg tea.Msg) tea.Cmd {
	switch a.inputMode {
	case inputModeCommand:
		return a.updateCommandPrompt(msg)
	case inputModeScale:
		return a.updateScalePrompt(msg)
	case inputModeLocalPort:
		return a.updateLocalPortPrompt(msg)
	default:
		return a.updateSearchPrompt(msg)
	}
}

func (a *App) updateKey(msg tea.KeyMsg) tea.Cmd {
	a.statusMessage = ""

	switch msg.String() {
	case "ctrl+c", "q":
		return tea.Quit
	case ":":
		if a.screen != screenContexts && a.screen != screenActionPicker && a.screen != screenConfirmAction {
			a.openCommands()
		}
		return nil
	case "/":
		if a.screen != screenActionPicker && a.screen != screenConfirmAction {
			a.openFilter()
		}
		return nil
	}

	switch a.screen {
	case screenContexts:
		return a.updateContextKeys(msg)
	case screenCatalog:
		return a.updateCatalogKeys(msg)
	case screenGroupResources:
		return a.updateGroupKeys(msg)
	case screenPods:
		return a.updatePodKeys(msg)
	case screenResourceList:
		return a.updateResourceListKeys(msg)
	case screenPodDetails:
		return a.updatePodDetailKeys(msg)
	case screenResourceDetails:
		return a.updateResourceDetailKeys(msg)
	case screenCommands:
		return a.updateCommandKeys(msg)
	case screenActionPicker:
		return a.updateActionPickerKeys(msg)
	case screenConfirmAction:
		return a.updateConfirmActionKeys(msg)
	}

	return nil
}

func (a *App) updateContextKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		a.navTable.MoveDown(1)
	case "k", "up":
		a.navTable.MoveUp(1)
	case "g", "home":
		a.navTable.MoveTop()
	case "G", "end":
		a.navTable.MoveBottom()
	case " ":
		index := a.navTable.SelectedIndex()
		if index >= 0 && index < len(a.visibleContexts) {
			name := a.visibleContexts[index].Name
			if a.selectedContext[name] {
				delete(a.selectedContext, name)
			} else {
				a.selectedContext[name] = true
			}
			a.refreshContextRows()
		}
	case "esc":
		if a.contextQuery != "" {
			a.contextQuery = ""
			a.refreshContextRows()
		}
	case "enter":
		if a.connecting {
			return nil
		}
		selected := a.selectedContextNames()
		if len(selected) == 0 {
			a.statusMessage = "select at least one context"
			return nil
		}
		a.connecting = true
		a.activity = "connecting contexts"
		return connectContextsCmd(a.manager, selected)
	}
	return nil
}

func (a *App) updateCatalogKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		a.navTable.MoveDown(1)
	case "k", "up":
		a.navTable.MoveUp(1)
	case "g", "home":
		a.navTable.MoveTop()
	case "G", "end":
		a.navTable.MoveBottom()
	case "enter":
		index := a.navTable.SelectedIndex()
		if index >= 0 && index < len(a.visibleGroups) {
			a.activeGroup = a.visibleGroups[index]
			a.screen = screenGroupResources
			a.resourceQuery = ""
			a.refreshGroupResources()
		}
	case "esc", "backspace":
		if a.catalogQuery != "" {
			a.catalogQuery = ""
			a.refreshCatalog()
			return nil
		}
		a.openContextPicker(pickerModeAdd)
	}
	return nil
}

func (a *App) updateGroupKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		a.navTable.MoveDown(1)
	case "k", "up":
		a.navTable.MoveUp(1)
	case "g", "home":
		a.navTable.MoveTop()
	case "G", "end":
		a.navTable.MoveBottom()
	case "enter":
		index := a.navTable.SelectedIndex()
		if index >= 0 && index < len(a.visibleResources) {
			a.openResourceList(a.visibleResources[index])
		}
	case "esc", "backspace":
		if a.resourceQuery != "" {
			a.resourceQuery = ""
			a.refreshGroupResources()
			return nil
		}
		a.screen = screenCatalog
		a.refreshCatalog()
	}
	return nil
}

func (a *App) updatePodKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		a.podTable.MoveDown(1)
	case "k", "up":
		a.podTable.MoveUp(1)
	case "g", "home":
		a.podTable.MoveTop()
	case "G", "end":
		a.podTable.MoveBottom()
	case "pgdown", "f":
		a.podTable.MoveDown(max(1, a.listPageSize()))
	case "pgup", "b":
		a.podTable.MoveUp(max(1, a.listPageSize()))
	case "tab":
		a.advanceNamespace(1)
	case "shift+tab":
		a.advanceNamespace(-1)
	case "a":
		a.namespace = ""
		a.refreshPods(time.Now())
	case "o":
		a.cyclePodSort()
		a.refreshPods(time.Now())
	case "O", "shift+o":
		a.togglePodSortReverse()
		a.refreshPods(time.Now())
	case "enter":
		row, ok := a.podRowAt(a.podTable.SelectedIndex(), time.Now())
		if !ok {
			a.statusMessage = "pod vanished during refresh"
			return nil
		}
		details, ok := a.store.PodDetailsByKey(row.Key, time.Now())
		if !ok {
			a.statusMessage = "pod vanished during refresh"
			return nil
		}
		a.activePod = details
		a.lastDataVersion = a.store.Version()
		a.lastTick = time.Now()
		a.screen = screenPodDetails
	case "esc", "backspace":
		if a.podQuery != "" {
			a.podQuery = ""
			a.refreshPods(time.Now())
			return nil
		}
		a.screen = screenGroupResources
		a.refreshGroupResources()
	}
	return nil
}

func (a *App) updateResourceListKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		a.resourceTable.MoveDown(1)
	case "k", "up":
		a.resourceTable.MoveUp(1)
	case "g", "home":
		a.resourceTable.MoveTop()
	case "G", "end":
		a.resourceTable.MoveBottom()
	case "pgdown", "f":
		a.resourceTable.MoveDown(max(1, a.listPageSize()))
	case "pgup", "b":
		a.resourceTable.MoveUp(max(1, a.listPageSize()))
	case "tab":
		if a.activeResource.Namespaced {
			a.advanceNamespace(1)
		}
	case "shift+tab":
		if a.activeResource.Namespaced {
			a.advanceNamespace(-1)
		}
	case "a":
		if a.activeResource.Namespaced {
			a.namespace = ""
			a.refreshResourceList(time.Now())
		}
	case "o":
		a.cycleResourceSort()
		a.refreshResourceList(time.Now())
	case "O", "shift+o":
		a.toggleResourceSortReverse()
		a.refreshResourceList(time.Now())
	case "enter":
		if !a.openCurrentResourceSelection(a.resourceTable.SelectedIndex(), time.Now()) {
			a.statusMessage = "resource vanished during refresh"
		}
	case "esc", "backspace":
		if a.resourceQuery2 != "" {
			a.resourceQuery2 = ""
			a.refreshResourceList(time.Now())
			return nil
		}
		a.screen = screenGroupResources
		a.refreshGroupResources()
	}
	return nil
}

func (a *App) updatePodDetailKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "backspace":
		a.screen = screenPods
		a.refreshPods(time.Now())
	case "e":
		return a.runExecPod()
	case "p":
		return a.runPortForwardPod()
	case "y":
		return a.runEditPod()
	}
	return nil
}

func (a *App) updateResourceDetailKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "backspace":
		if a.isResourceListKindImplemented() {
			a.screen = screenResourceList
			a.refreshResourceList(time.Now())
			return nil
		}
		a.screen = screenGroupResources
		a.refreshGroupResources()
	case "p":
		return a.runPortForwardResource()
	case "y":
		return a.runEditResource()
	case "s":
		return a.openScalePrompt()
	case "r":
		return a.runRestartResource()
	}
	return nil
}

func (a *App) updateCommandKeys(msg tea.KeyMsg) tea.Cmd {
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
		if a.commandQuery != "" {
			a.commandQuery = ""
			a.refreshCommands()
			return nil
		}
		a.inputMode = inputModeSearch
		a.filter.Deactivate()
		a.screen = a.prevScreen
		a.refreshCurrentScreen(time.Now())
	case "enter":
		index := a.navTable.SelectedIndex()
		if index >= 0 && index < len(a.visibleCommands) {
			command := a.visibleCommands[index]
			a.inputMode = inputModeSearch
			a.filter.Deactivate()
			command.Run(a)
		}
	}
	return nil
}

func (a *App) currentView() (string, string, string) {
	switch a.screen {
	case screenContexts:
		mode := "select contexts"
		if a.pickerMode == pickerModeAdd {
			mode = "add contexts"
		}
		return "surfsk8s · " + mode, a.navTable.View(), "space toggle  / filter  esc clear-find  enter connect  q quit"
	case screenCatalog:
		return "surfsk8s · resource catalog", a.navTable.View(), "enter open group  : commands  / filter  esc clear/back"
	case screenGroupResources:
		return "surfsk8s · " + a.activeGroup.Name, a.navTable.View(), "enter open resource  : commands  / filter  esc clear/back"
	case screenPods:
		footer := a.podTable.Footer() + "  sort:" + a.podSort.Label() + "  o next-sort  O reverse  / filter  tab ns  a all  enter details  esc clear/back"
		return "surfsk8s · pods", a.podTable.View(), footer
	case screenResourceList:
		footer := a.resourceTable.Footer() + "  sort:" + a.currentResourceSort().Label() + "  o next-sort  O reverse  / filter  enter details  esc clear/back"
		if a.activeResource.Namespaced {
			footer += "  tab ns  a all"
		}
		return "surfsk8s · " + strings.ToLower(a.activeResource.Display), a.resourceTable.View(), footer
	case screenPodDetails:
		return "surfsk8s · pod details", a.renderPodDetails(), "e exec  y edit  p port-forward  esc back"
	case screenResourceDetails:
		return "surfsk8s · resource details", a.renderResourceDetails(), a.resourceDetailFooter()
	case screenCommands:
		return "surfsk8s · commands", a.navTable.View(), "type to filter  j/k move  g/G edge  enter run  esc clear/close"
	case screenActionPicker:
		return "surfsk8s · " + a.actionPickerTitle, a.navTable.View(), a.actionPickerFooter
	case screenConfirmAction:
		return "surfsk8s · confirm action", a.renderConfirmAction(), "enter confirm  esc cancel"
	default:
		return "surfsk8s", "", ""
	}
}

func (a *App) openFilter() {
	prompt := "/"
	placeholder := "filter (* ? =exact)"
	query := a.currentQuery()
	if a.screen == screenCommands {
		prompt = ":"
		placeholder = "command"
		query = a.commandQuery
		a.inputMode = inputModeCommand
	} else {
		a.inputMode = inputModeSearch
	}

	a.filter.SetPrompt(prompt)
	a.filter.SetPlaceholder(placeholder)
	a.filter.SetValue(query)
	a.filter.Activate()
}

func (a *App) openCommands() {
	a.prevScreen = a.screen
	a.screen = screenCommands
	a.inputMode = inputModeCommand
	a.filter.SetPrompt(":")
	a.filter.SetPlaceholder("command")
	a.filter.SetValue(a.commandQuery)
	a.filter.Activate()
	a.refreshCommands()
}

func (a *App) openContextPicker(mode pickerMode) {
	a.prevScreen = a.screen
	a.screen = screenContexts
	a.pickerMode = mode
	if mode == pickerModeAdd {
		for _, name := range a.manager.ConnectedContextNames() {
			a.selectedContext[name] = true
		}
	}
	a.refreshContextRows()
}

func (a *App) openResourceList(resource cluster.ResourceKind) {
	a.activeResource = resource
	switch {
	case isCorePods(resource):
		a.screen = screenPods
		a.podQuery = ""
		a.refreshPods(time.Now())
	case isBuiltInResourceList(resource):
		a.screen = screenResourceList
		a.resourceQuery2 = ""
		a.refreshResourceList(time.Now())
	default:
		a.screen = screenResourceDetails
	}
}

func (a *App) refreshCurrentScreen(now time.Time) {
	switch a.screen {
	case screenContexts:
		a.refreshContextRows()
	case screenCatalog:
		a.refreshCatalog()
	case screenGroupResources:
		a.refreshGroupResources()
	case screenPods:
		a.refreshPods(now)
	case screenResourceList:
		a.refreshResourceList(now)
	case screenPodDetails:
		a.refreshActivePodDetails(now)
	case screenResourceDetails:
		a.refreshActiveResourceDetails(now)
	case screenCommands:
		a.refreshCommands()
	case screenActionPicker:
		a.refreshActionPicker()
	case screenConfirmAction:
		return
	}
}

func (a *App) refreshContextRows() {
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = time.Now()
	matches := fuzzyContexts(a.contexts, a.contextQuery)
	a.visibleContexts = matches
	a.visibleRows = len(matches)
	a.totalRows = len(a.contexts)
	a.setNavTable("CONTEXTS", renderContextRows(matches, a.selectedContext, a.pickerMode))
}

func (a *App) refreshCatalog() {
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = time.Now()
	a.catalog = a.manager.Catalog()
	groups := fuzzyGroups(a.catalog, a.catalogQuery)
	a.visibleGroups = groups
	a.visibleRows = len(groups)
	a.totalRows = len(a.catalog)
	a.setNavTable("GROUPS", renderGroupRows(groups))
}

func (a *App) refreshGroupResources() {
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = time.Now()
	resources := fuzzyResources(a.activeGroup.Resources, a.resourceQuery)
	a.visibleResources = resources
	a.visibleRows = len(resources)
	a.totalRows = len(a.activeGroup.Resources)
	a.setNavTable(strings.ToUpper(a.activeGroup.Name), renderResourceRows(resources))
}

func (a *App) refreshPods(now time.Time) {
	total := 0
	filtered := 0
	namespaces := a.store.PodNamespaces()
	if a.podNeedsMaterializedSort() {
		total, filtered = a.buildSortedPods()
	} else {
		a.sortedPods = a.sortedPods[:0]
		a.store.ForEachPod(func(row state.PodRow) bool {
			total++
			if !a.matchPodRow(row) {
				return true
			}
			filtered++
			return true
		})
	}

	a.lastDataVersion = a.store.Version()
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = now
	a.namespaces = namespaces
	a.visibleRows = filtered
	a.totalRows = total
	a.podTable.SetEmptyMessage(a.emptyMessageFor("pods"))
	a.podTable.SetWindowProvider(filtered, func(start int, end int) [][]string {
		return a.podsView.Rows(a.podWindow(start, end-start, time.Now()))
	})
}

func (a *App) refreshResourceList(now time.Time) {
	a.lastDataVersion = a.store.Version()
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = now
	switch a.activeResource.Resource {
	case "deployments":
		a.namespaces = a.store.DeploymentNamespaces()
		total := 0
		filtered := 0
		if a.deploymentNeedsMaterializedSort() {
			total, filtered = a.buildSortedDeployments()
		} else {
			a.sortedDeployments = a.sortedDeployments[:0]
			total, filtered = a.countDeployments()
		}
		a.visibleRows = filtered
		a.totalRows = total
		a.resourceTable.SetColumns(a.deploymentsView.Columns())
		a.resourceTable.SetEmptyMessage(a.emptyMessageFor("deployments"))
		a.resourceTable.SetWindowProvider(filtered, func(start int, end int) [][]string {
			return a.deploymentsView.Rows(a.deploymentWindow(start, end-start, time.Now()))
		})
	case "services":
		a.namespaces = a.store.ServiceNamespaces()
		total := 0
		filtered := 0
		if a.serviceNeedsMaterializedSort() {
			total, filtered = a.buildSortedServices()
		} else {
			a.sortedServices = a.sortedServices[:0]
			total, filtered = a.countServices()
		}
		a.visibleRows = filtered
		a.totalRows = total
		a.resourceTable.SetColumns(a.servicesView.Columns())
		a.resourceTable.SetEmptyMessage(a.emptyMessageFor("services"))
		a.resourceTable.SetWindowProvider(filtered, func(start int, end int) [][]string {
			return a.servicesView.Rows(a.serviceWindow(start, end-start, time.Now()))
		})
	case "nodes":
		a.namespaces = []string{""}
		total := 0
		filtered := 0
		if a.nodeNeedsMaterializedSort() {
			total, filtered = a.buildSortedNodes()
		} else {
			a.sortedNodes = a.sortedNodes[:0]
			total, filtered = a.countNodes()
		}
		a.visibleRows = filtered
		a.totalRows = total
		a.resourceTable.SetColumns(a.nodesView.Columns())
		a.resourceTable.SetEmptyMessage(a.emptyMessageFor("nodes"))
		a.resourceTable.SetWindowProvider(filtered, func(start int, end int) [][]string {
			return a.nodesView.Rows(a.nodeWindow(start, end-start, time.Now()))
		})
	default:
		a.visibleRows = 0
		a.totalRows = 0
		a.resourceTable.SetWindowProvider(0, nil)
	}
}

func (a *App) refreshCommands() {
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = time.Now()
	commands := fuzzyCommands(a.commands, a.commandQuery)
	a.visibleCommands = commands
	a.visibleRows = len(commands)
	a.totalRows = len(a.commands)
	a.setNavTable("COMMANDS", renderCommandRows(commands))
}

func (a *App) refreshActivePodDetails(now time.Time) {
	storeVersion := a.store.Version()
	if storeVersion == a.lastDataVersion {
		return
	}
	resourceVersion, ok := a.store.PodResourceVersionByKey(a.activePod.Row.Key)
	if !ok {
		a.activePod = state.PodDetails{}
		a.lastDataVersion = storeVersion
		a.lastManagerVersion = a.manager.Version()
		return
	}
	if resourceVersion != a.activePod.Row.ResourceVersion {
		updated, ok := a.store.PodDetailsByKey(a.activePod.Row.Key, now)
		if ok {
			a.activePod = updated
		}
	}
	a.lastDataVersion = storeVersion
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = now
}

func (a *App) refreshActiveResourceDetails(now time.Time) {
	if !a.isResourceListKindImplemented() {
		return
	}
	storeVersion := a.store.Version()
	if storeVersion == a.lastDataVersion {
		return
	}
	if !a.refreshCurrentDetail(now) {
		a.statusMessage = "resource vanished during refresh"
	}
	a.lastDataVersion = storeVersion
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = now
}

func (a *App) refreshCurrentDetail(now time.Time) bool {
	switch a.activeResource.Resource {
	case "deployments":
		row, ok := a.store.DeploymentDetailsByKey(a.activeDeployment.Row.Key, now)
		if !ok {
			return false
		}
		a.activeDeployment = row
		return true
	case "services":
		row, ok := a.store.ServiceDetailsByKey(a.activeService.Row.Key, now)
		if !ok {
			return false
		}
		a.activeService = row
		return true
	case "nodes":
		row, ok := a.store.NodeDetailsByKey(a.activeNode.Row.Key, now)
		if !ok {
			return false
		}
		a.activeNode = row
		return true
	default:
		return false
	}
}

func (a *App) openCurrentResourceSelection(index int, now time.Time) bool {
	switch a.activeResource.Resource {
	case "deployments":
		row, ok := a.deploymentRowAt(index, now)
		if !ok {
			return false
		}
		details, ok := a.store.DeploymentDetailsByKey(row.Key, now)
		if !ok {
			return false
		}
		a.activeDeployment = details
	case "services":
		row, ok := a.serviceRowAt(index, now)
		if !ok {
			return false
		}
		details, ok := a.store.ServiceDetailsByKey(row.Key, now)
		if !ok {
			return false
		}
		a.activeService = details
	case "nodes":
		row, ok := a.nodeRowAt(index, now)
		if !ok {
			return false
		}
		details, ok := a.store.NodeDetailsByKey(row.Key, now)
		if !ok {
			return false
		}
		a.activeNode = details
	default:
		return false
	}
	a.screen = screenResourceDetails
	a.lastDataVersion = a.store.Version()
	a.lastTick = now
	return true
}

func (a *App) setNavTable(title string, rows [][]string) {
	width := max(24, a.width-4)
	a.navTable.SetColumns([]components.Column{{Title: title, Width: width}})
	a.navTable.SetRows(rows)
	a.navTable.SetSize(width, max(4, a.height-2))
}

func (a *App) resizeTables() {
	a.navTable.SetSize(max(24, a.width-4), max(4, a.height-2))
	a.podTable.SetSize(a.width, max(6, a.height-2))
	a.resourceTable.SetSize(a.width, max(6, a.height-2))
}

func (a *App) renderPodDetails() string {
	pod := a.activePod.Pod
	if pod == nil {
		return "pod disappeared"
	}

	sections := []string{
		fmt.Sprintf("Name:      %s", a.activePod.Row.Name),
		fmt.Sprintf("Namespace: %s", a.activePod.Row.Namespace),
		fmt.Sprintf("Cluster:   %s", a.activePod.Row.Cluster),
		fmt.Sprintf("Status:    %s", a.activePod.Row.Status),
		fmt.Sprintf("Ready:     %s", a.activePod.Row.Ready),
		fmt.Sprintf("Restarts:  %d", a.activePod.Row.Restarts),
		fmt.Sprintf("Node:      %s", a.activePod.Row.Node),
		fmt.Sprintf("Age:       %s", a.activePod.Row.Age),
	}

	if len(pod.Spec.Containers) != 0 {
		sections = append(sections, "", "Containers:")
		for _, container := range pod.Spec.Containers {
			sections = append(sections, "- "+container.Name+renderContainerPorts(container))
		}
	}
	if len(pod.Labels) != 0 {
		sections = append(sections, "", "Labels:")
		keys := sortedMapKeys(pod.Labels)
		for _, key := range keys {
			sections = append(sections, fmt.Sprintf("- %s=%s", key, pod.Labels[key]))
		}
	}
	return strings.Join(sections, "\n")
}

func (a *App) renderResourceDetails() string {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		return renderDeploymentDetails(a.activeDeployment)
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		return renderServiceDetails(a.activeService)
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		return renderNodeDetails(a.activeNode)
	default:
		resource := a.activeResource
		sections := []string{
			fmt.Sprintf("Resource:    %s", resource.Display),
			fmt.Sprintf("Kind:        %s", resource.Kind),
			fmt.Sprintf("API group:   %s", defaultString(resource.APIGroup, "core")),
			fmt.Sprintf("API version: %s", defaultString(resource.Version, "server-default")),
			fmt.Sprintf("Plural:      %s", resource.Resource),
			fmt.Sprintf("Scope:       %s", scopeLabel(resource.Namespaced)),
			"",
			"Informer-backed listing not implemented yet for this kind.",
			"Pod, deployment, service, node browsing are implemented.",
		}
		if resource.Custom {
			sections = append(sections, "", "Discovered from CRD API discovery.")
		}
		return strings.Join(sections, "\n")
	}
}

func (a *App) resourceDetailFooter() string {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		return "s scale  r restart  y edit  esc back"
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		return "p port-forward  y edit  esc back"
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		return "y edit  esc back"
	default:
		return "esc back"
	}
}

func (a *App) shouldRefresh(now time.Time) bool {
	if a.manager.Version() != a.lastManagerVersion {
		return true
	}

	switch a.screen {
	case screenPods, screenResourceList:
		if a.store.Version() != a.lastDataVersion {
			return true
		}
		return now.Sub(a.lastTick) >= time.Second
	case screenPodDetails, screenResourceDetails:
		return a.store.Version() != a.lastDataVersion
	case screenCatalog, screenGroupResources, screenContexts, screenCommands:
		return now.Sub(a.lastTick) >= time.Second
	default:
		return false
	}
}

func (a *App) advanceNamespace(delta int) {
	if delta == 0 {
		panic("app.App.advanceNamespace: zero delta")
	}
	if len(a.namespaces) == 0 {
		a.namespace = ""
		return
	}

	current := 0
	for idx, namespace := range a.namespaces {
		if namespace == a.namespace {
			current = idx
			break
		}
	}

	next := current + delta
	if next < 0 {
		next = len(a.namespaces) - 1
	}
	if next >= len(a.namespaces) {
		next = 0
	}
	a.namespace = a.namespaces[next]
	if a.screen == screenPods {
		a.refreshPods(time.Now())
		return
	}
	if a.screen == screenResourceList {
		a.refreshResourceList(time.Now())
	}
}

func (a *App) selectedContextNames() []string {
	names := make([]string, 0, len(a.selectedContext))
	for _, context := range a.contexts {
		if a.selectedContext[context.Name] {
			names = append(names, context.Name)
		}
	}
	return names
}

func (a *App) currentQuery() string {
	switch a.screen {
	case screenContexts:
		return a.contextQuery
	case screenCatalog:
		return a.catalogQuery
	case screenGroupResources:
		return a.resourceQuery
	case screenPods:
		return a.podQuery
	case screenResourceList:
		return a.resourceQuery2
	case screenCommands:
		return a.commandQuery
	default:
		return ""
	}
}

func (a *App) setCurrentQuery(value string) {
	switch a.screen {
	case screenContexts:
		a.contextQuery = value
	case screenCatalog:
		a.catalogQuery = value
	case screenGroupResources:
		a.resourceQuery = value
	case screenPods:
		a.podQuery = value
	case screenResourceList:
		a.resourceQuery2 = value
	case screenCommands:
		a.commandQuery = value
	}
}

func (a *App) currentNamespaceLabel() string {
	if !a.activeResource.Namespaced && (a.screen == screenResourceList || a.screen == screenResourceDetails) {
		return "cluster"
	}
	return a.namespace
}

func (a *App) isResourceListKindImplemented() bool {
	return isBuiltInResourceList(a.activeResource)
}

func tickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(at time.Time) tea.Msg {
		return tickMsg(at)
	})
}

func connectContextsCmd(manager *cluster.Manager, contexts []string) tea.Cmd {
	contextsCopy := append([]string(nil), contexts...)
	return func() tea.Msg {
		err := manager.Connect(context.Background(), contextsCopy)
		return connectResultMsg{contexts: contextsCopy, err: err}
	}
}

func renderContextRows(contexts []cluster.ContextInfo, selected map[string]bool, mode pickerMode) [][]string {
	rows := make([][]string, 0, len(contexts))
	for _, context := range contexts {
		mark := "[ ]"
		if selected[context.Name] {
			mark = "[x]"
		}
		current := ""
		if context.Current {
			current = " current"
		}
		label := fmt.Sprintf("%s %s  (%s · %s)%s", mark, context.Name, context.Cluster, context.User, current)
		if mode == pickerModeAdd && selected[context.Name] {
			label += "  connected"
		}
		rows = append(rows, []string{label})
	}
	return rows
}

func renderGroupRows(groups []cluster.ResourceGroup) [][]string {
	rows := make([][]string, 0, len(groups))
	for _, group := range groups {
		rows = append(rows, []string{fmt.Sprintf("%s  (%d resources)", group.Name, len(group.Resources))})
	}
	return rows
}

func renderResourceRows(resources []cluster.ResourceKind) [][]string {
	rows := make([][]string, 0, len(resources))
	for _, resource := range resources {
		scope := "cluster"
		if resource.Namespaced {
			scope = "ns"
		}
		apiGroup := defaultString(resource.APIGroup, "core")
		prefix := resource.Display
		if resource.Custom {
			prefix = resource.Display + "  [CRD]"
		}
		rows = append(rows, []string{fmt.Sprintf("%s  (%s · %s)", prefix, apiGroup, scope)})
	}
	return rows
}

func renderCommandRows(commands []commandItem) [][]string {
	rows := make([][]string, 0, len(commands))
	for _, command := range commands {
		rows = append(rows, []string{fmt.Sprintf("%s  -  %s", command.Name, command.Description)})
	}
	return rows
}

func fuzzyContexts(contexts []cluster.ContextInfo, query string) []cluster.ContextInfo {
	if strings.TrimSpace(query) == "" {
		return append([]cluster.ContextInfo(nil), contexts...)
	}
	matched := make([]scoredContext, 0, len(contexts))
	for _, context := range contexts {
		candidate := context.Name + " " + context.Cluster + " " + context.User
		score, ok := scoreSearchCandidate(candidate, query)
		if !ok {
			continue
		}
		matched = append(matched, scoredContext{context: context, score: score})
	}
	sort.Slice(matched, func(i int, j int) bool {
		if matched[i].score != matched[j].score {
			return matched[i].score > matched[j].score
		}
		return matched[i].context.Name < matched[j].context.Name
	})
	result := make([]cluster.ContextInfo, 0, len(matched))
	for _, item := range matched {
		result = append(result, item.context)
	}
	return result
}

type scoredContext struct {
	context cluster.ContextInfo
	score   int
}

func fuzzyGroups(groups []cluster.ResourceGroup, query string) []cluster.ResourceGroup {
	if strings.TrimSpace(query) == "" {
		return append([]cluster.ResourceGroup(nil), groups...)
	}
	matched := make([]scoredGroup, 0, len(groups))
	for _, group := range groups {
		candidate := group.Name
		for _, resource := range group.Resources {
			candidate += " " + resource.Display
		}
		score, ok := scoreSearchCandidate(candidate, query)
		if !ok {
			continue
		}
		matched = append(matched, scoredGroup{group: group, score: score})
	}
	sort.SliceStable(matched, func(i int, j int) bool { return matched[i].score > matched[j].score })
	result := make([]cluster.ResourceGroup, 0, len(matched))
	for _, item := range matched {
		result = append(result, item.group)
	}
	return result
}

type scoredGroup struct {
	group cluster.ResourceGroup
	score int
}

func fuzzyResources(resources []cluster.ResourceKind, query string) []cluster.ResourceKind {
	if strings.TrimSpace(query) == "" {
		return append([]cluster.ResourceKind(nil), resources...)
	}
	matched := make([]scoredResource, 0, len(resources))
	for _, resource := range resources {
		candidate := resource.Display + " " + resource.Kind + " " + resource.Resource + " " + resource.APIGroup
		score, ok := scoreSearchCandidate(candidate, query)
		if !ok {
			continue
		}
		matched = append(matched, scoredResource{resource: resource, score: score})
	}
	sort.SliceStable(matched, func(i int, j int) bool { return matched[i].score > matched[j].score })
	result := make([]cluster.ResourceKind, 0, len(matched))
	for _, item := range matched {
		result = append(result, item.resource)
	}
	return result
}

type scoredResource struct {
	resource cluster.ResourceKind
	score    int
}

func fuzzyCommands(commands []commandItem, query string) []commandItem {
	if strings.TrimSpace(query) == "" {
		return append([]commandItem(nil), commands...)
	}
	matched := make([]scoredCommand, 0, len(commands))
	for _, command := range commands {
		score, ok := scoreSearchCandidate(command.Name+" "+command.Description, query)
		if !ok {
			continue
		}
		matched = append(matched, scoredCommand{command: command, score: score})
	}
	sort.SliceStable(matched, func(i int, j int) bool { return matched[i].score > matched[j].score })
	result := make([]commandItem, 0, len(matched))
	for _, item := range matched {
		result = append(result, item.command)
	}
	return result
}

type scoredCommand struct {
	command commandItem
	score   int
}

func renderContainerPorts(container corev1.Container) string {
	if len(container.Ports) == 0 {
		return ""
	}
	ports := make([]string, 0, len(container.Ports))
	for _, port := range container.Ports {
		ports = append(ports, fmt.Sprintf("%d/%s", port.ContainerPort, port.Protocol))
	}
	return "  ports=" + strings.Join(ports, ",")
}

func renderDeploymentDetails(details state.DeploymentDetails) string {
	deployment := details.Deployment
	if deployment == nil {
		return "deployment disappeared"
	}
	sections := []string{
		fmt.Sprintf("Name:      %s", details.Row.Name),
		fmt.Sprintf("Namespace: %s", details.Row.Namespace),
		fmt.Sprintf("Cluster:   %s", details.Row.Cluster),
		fmt.Sprintf("Ready:     %s", details.Row.Ready),
		fmt.Sprintf("Updated:   %d", details.Row.UpToDate),
		fmt.Sprintf("Available: %d", details.Row.Available),
		fmt.Sprintf("Age:       %s", details.Row.Age),
	}
	if len(deployment.Spec.Template.Spec.Containers) != 0 {
		sections = append(sections, "", "Containers:")
		for _, container := range deployment.Spec.Template.Spec.Containers {
			sections = append(sections, fmt.Sprintf("- %s  image=%s%s", container.Name, container.Image, renderContainerPorts(container)))
		}
	}
	return strings.Join(sections, "\n")
}

func renderServiceDetails(details state.ServiceDetails) string {
	service := details.Service
	if service == nil {
		return "service disappeared"
	}
	sections := []string{
		fmt.Sprintf("Name:       %s", details.Row.Name),
		fmt.Sprintf("Namespace:  %s", details.Row.Namespace),
		fmt.Sprintf("Cluster:    %s", details.Row.Cluster),
		fmt.Sprintf("Type:       %s", details.Row.Type),
		fmt.Sprintf("Cluster IP: %s", details.Row.ClusterIP),
		fmt.Sprintf("Ports:      %s", details.Row.Ports),
		fmt.Sprintf("Age:        %s", details.Row.Age),
	}
	if len(service.Spec.Selector) != 0 {
		sections = append(sections, "", "Selector:")
		keys := sortedMapKeys(service.Spec.Selector)
		for _, key := range keys {
			sections = append(sections, fmt.Sprintf("- %s=%s", key, service.Spec.Selector[key]))
		}
	}
	return strings.Join(sections, "\n")
}

func renderNodeDetails(details state.NodeDetails) string {
	node := details.Node
	if node == nil {
		return "node disappeared"
	}
	sections := []string{
		fmt.Sprintf("Name:    %s", details.Row.Name),
		fmt.Sprintf("Cluster: %s", details.Row.Cluster),
		fmt.Sprintf("Status:  %s", details.Row.Status),
		fmt.Sprintf("Roles:   %s", details.Row.Roles),
		fmt.Sprintf("Version: %s", details.Row.Version),
		fmt.Sprintf("Age:     %s", details.Row.Age),
	}
	if internalIP := nodeAddress(node, corev1.NodeInternalIP); internalIP != "" {
		sections = append(sections, fmt.Sprintf("Internal IP: %s", internalIP))
	}
	if len(node.Labels) != 0 {
		sections = append(sections, "", "Labels:")
		keys := sortedMapKeys(node.Labels)
		for _, key := range keys {
			sections = append(sections, fmt.Sprintf("- %s=%s", key, node.Labels[key]))
		}
	}
	return strings.Join(sections, "\n")
}

func nodeAddress(node *corev1.Node, addressType corev1.NodeAddressType) string {
	for _, address := range node.Status.Addresses {
		if address.Type == addressType {
			return address.Address
		}
	}
	return ""
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func scopeLabel(namespaced bool) string {
	if namespaced {
		return "Namespaced"
	}
	return "Cluster"
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func (a *App) String() string {
	return fmt.Sprintf("App{screen:%d, rows:%d}", a.screen, a.visibleRows)
}

func (a *App) listPageSize() int {
	if a.height <= 6 {
		return 1
	}
	return a.height - 6
}

func (a *App) matchPodRow(row state.PodRow) bool {
	if a.namespace != "" && row.Namespace != a.namespace {
		return false
	}
	return matchesSearch(row.SearchText(), a.podQuery)
}

func (a *App) podWindow(start int, limit int, now time.Time) []state.PodRow {
	if a.podNeedsMaterializedSort() {
		return windowRowsFromSlice(a.sortedPods, start, limit, now)
	}
	if limit <= 0 {
		return nil
	}
	rows := make([]state.PodRow, 0, limit)
	matched := 0
	a.store.ForEachPod(func(row state.PodRow) bool {
		if !a.matchPodRow(row) {
			return true
		}
		if matched < start {
			matched++
			return true
		}
		if len(rows) >= limit {
			return false
		}
		rows = append(rows, row.WithAge(now))
		matched++
		return true
	})
	return rows
}

func (a *App) podRowAt(index int, now time.Time) (state.PodRow, bool) {
	rows := a.podWindow(index, 1, now)
	if len(rows) == 0 {
		return state.PodRow{}, false
	}
	return rows[0], true
}

func (a *App) countDeployments() (int, int) {
	total := 0
	filtered := 0
	a.store.ForEachDeployment(func(row state.DeploymentRow) bool {
		total++
		if a.namespace != "" && row.Namespace != a.namespace {
			return true
		}
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		filtered++
		return true
	})
	return total, filtered
}

func (a *App) deploymentWindow(start int, limit int, now time.Time) []state.DeploymentRow {
	if a.deploymentNeedsMaterializedSort() {
		return windowRowsFromSlice(a.sortedDeployments, start, limit, now)
	}
	if limit <= 0 {
		return nil
	}
	rows := make([]state.DeploymentRow, 0, limit)
	matched := 0
	a.store.ForEachDeployment(func(row state.DeploymentRow) bool {
		if a.namespace != "" && row.Namespace != a.namespace {
			return true
		}
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		if matched < start {
			matched++
			return true
		}
		if len(rows) >= limit {
			return false
		}
		rows = append(rows, row.WithAge(now))
		matched++
		return true
	})
	return rows
}

func (a *App) deploymentRowAt(index int, now time.Time) (state.DeploymentRow, bool) {
	rows := a.deploymentWindow(index, 1, now)
	if len(rows) == 0 {
		return state.DeploymentRow{}, false
	}
	return rows[0], true
}

func (a *App) countServices() (int, int) {
	total := 0
	filtered := 0
	a.store.ForEachService(func(row state.ServiceRow) bool {
		total++
		if a.namespace != "" && row.Namespace != a.namespace {
			return true
		}
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		filtered++
		return true
	})
	return total, filtered
}

func (a *App) serviceWindow(start int, limit int, now time.Time) []state.ServiceRow {
	if a.serviceNeedsMaterializedSort() {
		return windowRowsFromSlice(a.sortedServices, start, limit, now)
	}
	if limit <= 0 {
		return nil
	}
	rows := make([]state.ServiceRow, 0, limit)
	matched := 0
	a.store.ForEachService(func(row state.ServiceRow) bool {
		if a.namespace != "" && row.Namespace != a.namespace {
			return true
		}
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		if matched < start {
			matched++
			return true
		}
		if len(rows) >= limit {
			return false
		}
		rows = append(rows, row.WithAge(now))
		matched++
		return true
	})
	return rows
}

func (a *App) serviceRowAt(index int, now time.Time) (state.ServiceRow, bool) {
	rows := a.serviceWindow(index, 1, now)
	if len(rows) == 0 {
		return state.ServiceRow{}, false
	}
	return rows[0], true
}

func (a *App) countNodes() (int, int) {
	total := 0
	filtered := 0
	a.store.ForEachNode(func(row state.NodeRow) bool {
		total++
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		filtered++
		return true
	})
	return total, filtered
}

func (a *App) nodeWindow(start int, limit int, now time.Time) []state.NodeRow {
	if a.nodeNeedsMaterializedSort() {
		return windowRowsFromSlice(a.sortedNodes, start, limit, now)
	}
	if limit <= 0 {
		return nil
	}
	rows := make([]state.NodeRow, 0, limit)
	matched := 0
	a.store.ForEachNode(func(row state.NodeRow) bool {
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		if matched < start {
			matched++
			return true
		}
		if len(rows) >= limit {
			return false
		}
		rows = append(rows, row.WithAge(now))
		matched++
		return true
	})
	return rows
}

func (a *App) nodeRowAt(index int, now time.Time) (state.NodeRow, bool) {
	rows := a.nodeWindow(index, 1, now)
	if len(rows) == 0 {
		return state.NodeRow{}, false
	}
	return rows[0], true
}

func matchesSearch(candidate string, query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return true
	}
	_, ok := scoreSearchCandidate(candidate, query)
	return ok
}

func isCorePods(resource cluster.ResourceKind) bool {
	return resource.Resource == "pods" && resource.APIGroup == ""
}

func isBuiltInResourceList(resource cluster.ResourceKind) bool {
	switch {
	case resource.Resource == "deployments" && resource.APIGroup == "apps":
		return true
	case resource.Resource == "services" && resource.APIGroup == "":
		return true
	case resource.Resource == "nodes" && resource.APIGroup == "":
		return true
	default:
		return false
	}
}

func (a *App) emptyMessageFor(kind string) string {
	statuses := a.manager.Statuses()
	if len(statuses) != 0 {
		allConnecting := true
		for _, status := range statuses {
			if status.Synced && status.Healthy {
				allConnecting = false
				break
			}
			if status.Message != "" && status.Message != "connecting" {
				allConnecting = false
				break
			}
			if status.Warning != "" {
				allConnecting = false
				break
			}
		}
		if allConnecting {
			return "Syncing cluster caches..."
		}
	}

	switch kind {
	case "pods":
		return "No pods"
	case "deployments":
		return "No deployments"
	case "services":
		return "No services"
	case "nodes":
		return "No nodes"
	default:
		return "No items"
	}
}
