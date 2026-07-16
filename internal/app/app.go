package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
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
	Resume           bool
	Context          context.Context
}

// genericResourceBackend is the narrow cluster surface used by generic list/detail/jump effects.
type genericResourceBackend interface {
	ListGenericResource(ctx context.Context, resource cluster.ResourceKind) ([]cluster.GenericResourceRow, error)
	GenericResourceDetails(ctx context.Context, resource cluster.ResourceKind, key cluster.GenericResourceKey, now time.Time) (cluster.GenericResourceDetails, error)
	GenericResourceVersion(resourceID string) uint64
	GenericResourceDelta(resourceID string, sinceVersion uint64) (uint64, []cluster.GenericResourceChange, bool)
	GenericResourceObjectVersion(resource cluster.ResourceKind, key cluster.GenericResourceKey) (string, bool)
	ForEachGenericResourceRow(ctx context.Context, resource cluster.ResourceKind, visit func(cluster.GenericResourceRow) bool) error
}

// logBackend is the narrow cluster surface used by log fetch effects.
type logBackend interface {
	PodLogsWithOptions(ctx context.Context, details state.PodDetails, options cluster.PodLogsOptions) (string, error)
	NodeLogWithOptions(ctx context.Context, details state.NodeDetails, options cluster.NodeLogOptions) (string, error)
	NodeLogEntries(ctx context.Context, details state.NodeDetails, dir string) ([]cluster.NodeLogEntry, error)
	DeploymentPodDetails(details state.DeploymentDetails, now time.Time) ([]state.PodDetails, error)
}

type tickMsg time.Time

type connectResultMsg struct {
	contexts []string
	err      error
}

type podUsageResultMsg struct {
	key   state.PodKey
	usage cluster.PodResourceUsage
}

type podUsageSnapshotMsg struct {
	scopeKey   string
	generation uint64
	usages     map[string]cluster.PodResourceUsage
}

type nodeUsageResultMsg struct {
	key   state.NodeKey
	usage cluster.NodeResourceUsage
}

type nodeUsageSnapshotMsg struct {
	scopeKey   string
	generation uint64
	usages     map[string]cluster.NodeResourceUsage
}

type catalogOverviewResultMsg struct {
	scopeKey       string
	storeVersion   uint64
	managerVersion uint64
	data           catalogOverviewData
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
	screenLogs
	screenCommands
	screenResourceFinder
	screenScopePicker
	screenActionPicker
	screenConfirmAction
	screenTableFilterColumnPicker
	screenTableFilterManager
	screenTableSortColumnPicker
	screenTableSortDirectionPicker
	screenTableSortManager
)

type detailFocusKind uint8

const (
	detailFocusContent detailFocusKind = iota
	detailFocusPods
)

type pickerMode int

const (
	pickerModeEnter pickerMode = iota
	pickerModeAdd
)

type scopePickerKind uint8

const (
	scopePickerNamespace scopePickerKind = iota
	scopePickerContext
)

type scopeOption struct {
	Label string
	Value string
}

type commandItem struct {
	Name        string
	Description string
	Run         func(*App)
}

// App is the top-level Bubbletea model.
// Orchestrates cluster connections, state, and UI views.
type App struct {
	store            *state.Store
	manager          *cluster.Manager
	genericBackend   genericResourceBackend
	logBackend       logBackend
	context          context.Context
	clusterStatusBuf []components.ClusterStatus
	executor         *actions.Executor

	podsView               views.PodsView
	deploymentsView        views.DeploymentsView
	servicesView           views.ServicesView
	nodesView              views.NodesView
	genericView            views.GenericResourcesView
	tableColumnPreferences map[string]map[string]tableColumnPreference

	navTable              components.Table
	podTable              components.Table
	resourceTable         components.Table
	detailPodTable        components.Table
	filter                components.Filter
	statusBar             components.StatusBar
	textViewport          viewport.Model
	textViewportContent   string
	textViewportLineCount int
	commands              []commandItem
	screen                screen
	prevScreen            screen
	podDetailReturnScreen screen
	pickerMode            pickerMode
	detailFocus           detailFocusKind

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

	logsReturnScreen      screen
	logTitle              string
	logLoading            bool
	logFetchedAt          time.Time
	logRequestToken       uint64
	nodeLogPickerToken    uint64
	nextAsyncToken        uint64
	logTarget             logTarget
	logRange              logRange
	logShowTimestamps     bool
	logWrap               bool
	logAutoRefreshPaused  bool
	logFilterQuery        string
	logCursor             time.Time
	logEntries            []logEntry
	logEntrySeen          map[string]struct{}
	logSelectedContainers map[string]bool
	logPod                state.PodDetails
	logDeployment         state.DeploymentDetails
	logNode               state.NodeDetails
	logNodePath           string
	logEntriesVersion     uint64
	logRenderedVersion    uint64
	logRenderedFilter     string
	logRenderedWidth      int
	logRenderedWrap       bool
	logRenderedTimestamps bool
	logRenderedContent    string
	logFollowOnRender     bool

	contexts            []cluster.ContextInfo
	visibleContexts     []cluster.ContextInfo
	selectedContext     map[string]bool
	contextQuery        string
	favoriteResourceIDs []string
	favoriteResources   map[string]bool

	catalog                       []cluster.ResourceGroup
	visibleGroups                 []cluster.ResourceGroup
	catalogQuery                  string
	catalogOverview               catalogOverviewData
	catalogOverviewFetchedAt      time.Time
	catalogOverviewLoading        bool
	catalogOverviewScopeKey       string
	catalogOverviewStoreVersion   uint64
	catalogOverviewManagerVersion uint64

	activeGroup      cluster.ResourceGroup
	visibleResources []cluster.ResourceKind
	resourceQuery    string
	activeResource   cluster.ResourceKind

	namespace                    string
	namespaces                   []string
	podNamespacesCacheKey        string
	deploymentNamespacesCacheKey string
	serviceNamespacesCacheKey    string
	podQuery                     string
	resourceQuery2               string
	activePod                    state.PodDetails
	activeDeployment             state.DeploymentDetails
	activeService                state.ServiceDetails
	activeNode                   state.NodeDetails
	activeDeploymentPods         []state.PodRow
	activeServicePods            []state.PodRow
	activeNodePods               []state.PodRow
	activeGenericDetails         cluster.GenericResourceDetails
	activePodUsage               cluster.PodResourceUsage
	activeNodeUsage              cluster.NodeResourceUsage
	podUsageByKey                map[string]cluster.PodResourceUsage
	nodeUsageByKey               map[string]cluster.NodeResourceUsage
	podUsageCellByKey            map[string]podUsageTableCells
	nodeUsageCellByKey           map[string]nodeUsageTableCells
	podTableRowBuf               [][]string
	deploymentTableRowBuf        [][]string
	serviceTableRowBuf           [][]string
	nodeTableRowBuf              [][]string
	podUsageFetchedAt            time.Time
	nodeUsageFetchedAt           time.Time
	podUsageListFetchedAt        time.Time
	nodeUsageListFetchedAt       time.Time
	podUsageListScopeKey         string
	nodeUsageListScopeKey        string
	podUsageListGeneration       uint64
	nodeUsageListGeneration      uint64
	podUsageLoading              bool
	nodeUsageLoading             bool
	podUsageListLoading          bool
	nodeUsageListLoading         bool
	lastDataVersion              uint64
	lastManagerVersion           uint64
	lastTick                     time.Time
	visibleRows                  int
	totalRows                    int

	genericRows                   []cluster.GenericResourceRow
	genericRowsByKey              map[string]cluster.GenericResourceRow
	genericVisibleKeySet          map[string]struct{}
	genericNamespaceCounts        map[string]int
	sortedGenericRows             []cluster.GenericResourceRow
	genericNamespaces             []string
	genericSort                   listSortState
	genericRowsResourceID         string
	genericCompiledColumns        []cluster.CompiledPrinterColumn
	genericCompiledColumnsVersion string
	genericPrinterValueCache      map[string][]string
	genericSearchTextCache        map[string]string
	genericFilterValueCache       map[string]map[int]string
	genericTableRowBuf            [][]string
	genericListCacheKey           string
	lastGenericFetchAt            time.Time
	genericDetailFetchedAt        time.Time

	visibleCommands      []commandItem
	commandQuery         string
	resourceFinderItems  []resourceFinderItem
	visibleResourceItems []resourceFinderItem
	resourceFinderQuery  string

	pendingFilterColumnIndex  int
	pendingFilterColumnTitle  string
	tableFilterColumnQuery    string
	visibleTableFilterColumns []tableFilterColumnOption
	visibleTableFilters       []tableColumnFilter
	podColumnFilters          []tableColumnFilter
	resourceColumnFilters     map[string][]tableColumnFilter
	nextTableFilterID         int

	pendingSortColumnIndex  int
	pendingSortColumnTitle  string
	tableSortColumnQuery    string
	visibleTableSortColumns []tableSortColumnOption
	visibleTableSorts       []tableSortCriterion
	podTableSorts           []tableSortCriterion
	resourceTableSorts      map[string][]tableSortCriterion
	nextTableSortID         int

	contextScope        string
	scopeReturnScreen   screen
	scopePickerKind     scopePickerKind
	scopeQuery          string
	scopeOptions        []scopeOption
	visibleScopeOptions []scopeOption

	podSort        listSortState
	deploymentSort listSortState
	serviceSort    listSortState
	nodeSort       listSortState

	sortedPods        []state.PodRow
	sortedDeployments []state.DeploymentRow
	sortedServices    []state.ServiceRow
	sortedNodes       []state.NodeRow

	resumeOnStart bool
	resumeSession sessionPreference
}

func New(store *state.Store, manager *cluster.Manager, cfg Config) *App {
	if store == nil {
		panic("app.New: nil store")
	}
	if manager == nil {
		panic("app.New: nil manager")
	}
	rootCtx := cfg.Context
	if rootCtx == nil {
		rootCtx = context.Background()
	}

	podsView := views.NewPodsView()
	deploymentsView := views.NewDeploymentsView()
	servicesView := views.NewServicesView()
	nodesView := views.NewNodesView()
	genericView := views.NewGenericResourcesView()
	navTable := components.NewTable([]components.Column{{Title: "ITEMS", Width: 80}})
	podTable := components.NewTable(podsView.Columns())
	podTable.SetVariant(components.TableVariantRich)
	podTable.SetEmptyMessage("No pods")
	resourceTable := components.NewTable(deploymentsView.Columns())
	resourceTable.SetVariant(components.TableVariantRich)
	detailPodTable := components.NewTable(podsView.Columns())
	detailPodTable.SetVariant(components.TableVariantRich)
	detailPodTable.SetEmptyMessage("No associated pods")
	filter := components.NewFilter()
	statusBar := components.NewStatusBar()
	textViewport := viewport.New(0, 0)
	contexts := manager.AvailableContexts()
	selectedContext := loadSelectedContexts(contexts)
	favoriteResourceIDs := loadFavoriteResourceIDs(manager.Catalog())
	tableColumnPreferences := loadTableColumnPreferences()
	favoriteResources := make(map[string]bool, len(favoriteResourceIDs))
	for _, id := range favoriteResourceIDs {
		favoriteResources[id] = true
	}

	app := &App{
		namespace:              cfg.InitialNamespace,
		store:                  store,
		manager:                manager,
		genericBackend:         manager,
		logBackend:             manager,
		context:                rootCtx,
		executor:               actions.NewExecutor(cfg.KubeconfigPath),
		podsView:               podsView,
		deploymentsView:        deploymentsView,
		servicesView:           servicesView,
		nodesView:              nodesView,
		genericView:            genericView,
		tableColumnPreferences: tableColumnPreferences,
		navTable:               navTable,
		podTable:               podTable,
		resourceTable:          resourceTable,
		detailPodTable:         detailPodTable,
		filter:                 filter,
		statusBar:              statusBar,
		textViewport:           textViewport,
		screen:                 screenContexts,
		pickerMode:             pickerModeEnter,
		contexts:               contexts,
		selectedContext:        selectedContext,
		favoriteResourceIDs:    favoriteResourceIDs,
		favoriteResources:      favoriteResources,
		namespaces:             []string{""},
		logRange:               logRangeLive,
		logShowTimestamps:      true,
		resumeOnStart:          cfg.Resume,
	}
	if cfg.Resume {
		prefs, err := loadPreferences()
		if err == nil {
			app.resumeSession = prefs.LastSession
		}
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
	if a.resumeOnStart {
		selected := a.selectedContextNames()
		if len(selected) != 0 {
			a.connecting = true
			a.activity = "connecting contexts"
			return tea.Batch(tickCmd(), connectContextsCmd(a.manager, selected))
		}
	}
	return tickCmd()
}

func loadSelectedContexts(contexts []cluster.ContextInfo) map[string]bool {
	selected := make(map[string]bool, len(contexts))
	prefs, err := loadPreferences()
	if err == nil && prefs.SelectedContextsSet {
		available := make(map[string]struct{}, len(contexts))
		for _, context := range contexts {
			available[context.Name] = struct{}{}
		}
		for _, name := range prefs.SelectedContexts {
			if _, ok := available[name]; ok {
				selected[name] = true
			}
		}
		return selected
	}
	if err == nil && len(prefs.SelectedContexts) != 0 {
		available := make(map[string]struct{}, len(contexts))
		for _, context := range contexts {
			available[context.Name] = struct{}{}
		}
		for _, name := range prefs.SelectedContexts {
			if _, ok := available[name]; ok {
				selected[name] = true
			}
		}
		if len(selected) != 0 {
			return selected
		}
	}
	for _, context := range contexts {
		if context.Current {
			selected[context.Name] = true
		}
	}
	return selected
}

func (a *App) persistSelectedContexts() {
	if err := updatePreferences(func(prefs *preferences) {
		prefs.SelectedContexts = a.selectedContextNames()
		prefs.SelectedContextsSet = true
	}); err != nil {
		a.statusMessage = err.Error()
	}
}

func loadFavoriteResourceIDs(catalog []cluster.ResourceGroup) []string {
	prefs, err := loadPreferences()
	if err == nil && prefs.FavoriteResourcesSet {
		return append([]string(nil), prefs.FavoriteResources...)
	}
	ids := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	for _, group := range catalog {
		for _, resource := range group.Resources {
			if !resource.Favorite {
				continue
			}
			if _, ok := seen[resource.ID]; ok {
				continue
			}
			seen[resource.ID] = struct{}{}
			ids = append(ids, resource.ID)
		}
	}
	return ids
}

func (a *App) persistFavoriteResources() {
	ids := append([]string(nil), a.favoriteResourceIDs...)
	if err := updatePreferences(func(prefs *preferences) {
		prefs.FavoriteResources = ids
		prefs.FavoriteResourcesSet = true
	}); err != nil {
		a.statusMessage = err.Error()
	}
}

func (a *App) persistSession() {
	session := sessionPreference{Namespace: a.namespace, ContextScope: a.contextScope}
	switch a.screen {
	case screenCatalog:
		session.Screen = "catalog"
		session.Query = a.catalogQuery
	case screenGroupResources:
		session.Screen = "group-resources"
		session.GroupName = a.activeGroup.Name
		session.Query = a.resourceQuery
	case screenPods:
		session.Screen = "pods"
		session.Query = a.podQuery
	case screenResourceList:
		session.Screen = "resource-list"
		session.ResourceID = a.activeResource.ID
		session.Query = a.resourceQuery2
	case screenResourceDetails:
		session.Screen = "resource-list"
		session.ResourceID = a.activeResource.ID
		session.Query = a.resourceQuery2
	case screenPodDetails, screenLogs:
		session.Screen = "pods"
		session.Query = a.podQuery
	default:
		session.Screen = "catalog"
	}
	if err := updatePreferences(func(prefs *preferences) { prefs.LastSession = session }); err != nil {
		a.statusMessage = err.Error()
	}
}

func (a *App) applyResumeSession(now time.Time) {
	session := a.resumeSession
	a.namespace = session.Namespace
	a.contextScope = session.ContextScope
	switch session.Screen {
	case "group-resources":
		if group, ok := a.catalogGroupByName(session.GroupName); ok {
			a.activeGroup = group
			a.resourceQuery = session.Query
			a.screen = screenGroupResources
			a.refreshGroupResources()
			return
		}
	case "pods":
		a.openResourceList(builtinPodResourceKind())
		a.podQuery = session.Query
		a.refreshPods(now)
		return
	case "resource-list":
		if resource, ok := a.catalogResourceByID(session.ResourceID); ok {
			a.resourceQuery2 = session.Query
			a.openResourceList(resource)
			a.setCurrentQuery(session.Query)
			a.refreshCurrentScreen(now)
			return
		}
	case "catalog":
		a.catalogQuery = session.Query
		a.screen = screenCatalog
		a.refreshCatalog()
		return
	}
	a.screen = screenCatalog
	a.refreshCatalog()
}

func (a *App) catalogGroupByName(name string) (cluster.ResourceGroup, bool) {
	for _, group := range a.manager.Catalog() {
		if group.Name == name {
			return group, true
		}
	}
	return cluster.ResourceGroup{}, false
}

func (a *App) catalogResourceByID(id string) (cluster.ResourceKind, bool) {
	for _, group := range a.manager.Catalog() {
		for _, resource := range group.Resources {
			if resource.ID == id {
				return resource, true
			}
		}
	}
	return cluster.ResourceKind{}, false
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = typed.Width
		a.height = typed.Height
		a.resizeTables()
		a.refreshCurrentScreen(time.Now())
		return a, nil

	case tea.KeyMsg:
		if typed.String() == "ctrl+c" {
			return a, tea.Quit
		}
		if a.filter.Active() {
			return a, a.updateFilter(typed)
		}
		return a, a.updateKey(typed)

	case tea.MouseMsg:
		return a, a.updateMouse(typed)

	case tickMsg:
		now := time.Time(typed)
		if a.shouldRefresh(now) {
			a.refreshCurrentScreen(now)
		}
		return a, tea.Batch(tickCmd(), a.maybeRefreshCatalogOverviewCmd(now), a.maybeRefreshResourceUsageCmd(now), a.maybeRefreshLogsCmd(now))

	case connectResultMsg:
		a.connecting = false
		a.activity = ""
		if typed.err != nil {
			a.statusMessage = typed.err.Error()
			return a, nil
		}
		a.persistSelectedContexts()
		a.statusMessage = fmt.Sprintf("connected %d context(s)", len(typed.contexts))
		if a.resumeOnStart {
			a.resumeOnStart = false
			a.applyResumeSession(time.Now())
		} else {
			a.screen = screenCatalog
			a.refreshCatalog()
		}
		return a, a.maybeRefreshCatalogOverviewCmd(time.Now())

	case actionResultMsg:
		if typed.err != nil {
			a.statusMessage = typed.description + ": " + typed.err.Error()
		} else {
			a.statusMessage = typed.description + " complete"
		}
		if (a.screen == screenResourceList || a.screen == screenResourceDetails) && !isBuiltInResourceList(a.activeResource) && a.supportsGenericResourceList(a.activeResource) {
			a.lastManagerVersion = 0
			a.lastGenericFetchAt = time.Time{}
		}
		a.activity = ""
		a.refreshCurrentScreen(time.Now())
		return a, nil

	case logsResultMsg:
		return a, a.handleLogsResult(typed)

	case nodeLogPickerResultMsg:
		return a, a.handleNodeLogPickerResult(typed)

	case podUsageResultMsg:
		a.podUsageLoading = false
		if a.screen == screenPodDetails && typed.key == a.activePod.Row.Key {
			a.activePodUsage = typed.usage
			a.podUsageFetchedAt = time.Now()
			a.refreshCurrentScreen(time.Now())
		}
		return a, nil

	case nodeUsageResultMsg:
		a.nodeUsageLoading = false
		if a.screen == screenResourceDetails && a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "" && typed.key == a.activeNode.Row.Key {
			a.activeNodeUsage = typed.usage
			a.nodeUsageFetchedAt = time.Now()
			a.refreshCurrentScreen(time.Now())
		}
		return a, nil

	case podUsageSnapshotMsg:
		a.podUsageListLoading = false
		if typed.scopeKey == a.podUsageScopeKey() && typed.generation == a.podUsageListGeneration {
			a.podUsageByKey = typed.usages
			a.podUsageCellByKey = nil
			a.podUsageListScopeKey = typed.scopeKey
			a.podUsageListFetchedAt = time.Now()
			if a.screen == screenPods {
				a.refreshPods(time.Now())
			}
		}
		return a, nil

	case nodeUsageSnapshotMsg:
		a.nodeUsageListLoading = false
		if typed.scopeKey == a.nodeUsageScopeKey() && typed.generation == a.nodeUsageListGeneration {
			a.nodeUsageByKey = typed.usages
			a.nodeUsageCellByKey = buildNodeUsageTableCellCache(typed.usages)
			a.nodeUsageListScopeKey = typed.scopeKey
			a.nodeUsageListFetchedAt = time.Now()
			if a.screen == screenResourceList && a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "" {
				a.refreshResourceList(time.Now())
			}
		}
		return a, nil

	case catalogOverviewResultMsg:
		a.catalogOverviewLoading = false
		if typed.scopeKey == a.catalogOverviewCurrentScopeKey() && typed.storeVersion == a.store.Version() && typed.managerVersion == a.manager.Version() {
			a.catalogOverview = typed.data
			a.catalogOverviewScopeKey = typed.scopeKey
			a.catalogOverviewStoreVersion = typed.storeVersion
			a.catalogOverviewManagerVersion = typed.managerVersion
			a.catalogOverviewFetchedAt = time.Now()
		}
		return a, nil
	}

	return a, nil
}

func (a *App) View() string {
	inputLabel, inputValue, inputActive := a.statusInputState()
	compactList := a.screen == screenPods || a.screen == screenResourceList
	catalogScreen := a.screen == screenCatalog

	a.clusterStatusBuf = a.manager.StatusesInto(a.clusterStatusBuf[:0])
	statusState := components.StatusBarState{
		Clusters:    a.clusterStatusBuf,
		Context:     a.currentContextLabel(),
		Namespace:   a.currentNamespaceLabel(),
		InputLabel:  inputLabel,
		InputValue:  inputValue,
		InputActive: inputActive,
		VisibleRows: a.visibleRows,
		TotalRows:   a.totalRows,
		Activity:    a.activity,
	}

	title, body, footer := a.currentView()

	sections := make([]string, 0, 8)
	if compactList {
		sections = append(sections, a.listScreenCombinedHeader(statusState))
	} else {
		sections = append(sections, theme.HeaderStyle.Render(title))
		sections = append(sections, a.statusBar.View(statusState))
	}
	if a.statusMessage != "" {
		sections = append(sections, theme.StatusWarn.Render(a.statusMessage))
	}
	if a.filter.Active() || strings.TrimSpace(a.currentQuery()) != "" {
		sections = append(sections, a.filter.View())
	}
	extraLines := 1
	if compactList {
		extraLines++
	}
	a.resizeTablesForBody(len(sections) + extraLines)
	if compactList {
		sections = append(sections, a.listScreenHeaderDivider())
	}

	if catalogScreen {
		body = a.renderCatalogBody()
	} else if a.screen == screenResourceDetails && a.resourceDetailHasPodTable() {
		body = a.renderResourceDetailBody(body, len(sections))
	} else if a.usesTextViewport() {
		body = a.renderTextViewport(body, len(sections))
	}

	sections = append(sections, body)
	if strings.TrimSpace(footer) != "" {
		sections = append(sections, theme.Muted.Render(footer))
	}
	return lipgloss.NewStyle().Padding(0, 1).Render(strings.Join(sections, "\n"))
}

func (a *App) listScreenHeaderDivider() string {
	if a.screen == screenPods {
		return a.podTable.TopDivider()
	}
	return a.resourceTable.TopDivider()
}

func (a *App) listScreenCombinedHeader(state components.StatusBarState) string {
	var b strings.Builder
	b.Grow(256)
	b.WriteString("surfsk8s")
	b.WriteString(" · ")
	b.WriteString(a.titleNamespaceSegment())
	b.WriteString(" · ")
	b.WriteString(a.listResourceTitleSegment())
	b.WriteString(" · ")
	if a.screen == screenPods {
		b.WriteString(a.podTable.Footer())
	} else {
		b.WriteString(a.resourceTable.Footer())
	}
	b.WriteString(" · ")
	b.WriteString(a.tableFilterFooter())
	b.WriteString(" · ")
	b.WriteString(a.tableSortLabel())
	if usage := a.listUsageStatus(time.Now()); usage != "" {
		b.WriteString(" · ")
		b.WriteString(usage)
	}
	leftLine := b.String()
	right := components.FormatListStatusRight(state)
	sep := theme.Muted.Render(" │ ")
	return theme.HeaderStyle.Render(leftLine) + sep + right
}

func (a *App) titleNamespaceSegment() string {
	ns := strings.TrimSpace(a.currentNamespaceLabel())
	if ns == "" {
		return "ns:all"
	}
	return "ns:" + ns
}

func (a *App) listResourceTitleSegment() string {
	if a.screen == screenPods {
		return "pods"
	}
	if !isBuiltInResourceList(a.activeResource) {
		return a.genericResourceTitle()
	}
	return strings.ToLower(a.activeResource.Display)
}

func (a *App) updateFilter(msg tea.Msg) tea.Cmd {
	switch a.inputMode {
	case inputModeCommand:
		return a.updateCommandPrompt(msg)
	case inputModeResourceFinder:
		return a.updateResourceFinderPrompt(msg)
	case inputModeScale:
		return a.updateScalePrompt(msg)
	case inputModeLocalPort:
		return a.updateLocalPortPrompt(msg)
	case inputModeScopePicker:
		return a.updateScopePickerPrompt(msg)
	case inputModeTableFilterValue:
		return a.updateTableFilterValuePrompt(msg)
	case inputModeLogExactFilter:
		return a.updateLogExactFilterPrompt(msg)
	case inputModeLogSavePath:
		return a.updateLogSavePrompt(msg)
	default:
		return a.updateSearchPrompt(msg)
	}
}

func (a *App) updateKey(msg tea.KeyMsg) tea.Cmd {
	a.statusMessage = ""

	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "q":
		a.persistSession()
		return tea.Quit
	case ":":
		if a.screen != screenContexts && a.screen != screenResourceFinder && a.screen != screenScopePicker && a.screen != screenActionPicker && a.screen != screenConfirmAction && a.screen != screenLogs {
			a.openCommands()
		}
		return nil
	case "/":
		if a.screen != screenResourceFinder && a.screen != screenScopePicker && a.screen != screenActionPicker && a.screen != screenConfirmAction && a.screen != screenTableFilterColumnPicker && a.screen != screenTableFilterManager && a.screen != screenTableSortColumnPicker && a.screen != screenTableSortDirectionPicker && a.screen != screenTableSortManager {
			a.openFilter()
		}
		return nil
	case "R":
		if a.screen == screenResourceList && a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps" {
			break
		}
		if a.screen != screenContexts && a.screen != screenActionPicker && a.screen != screenConfirmAction && a.screen != screenLogs && a.screen != screenTableFilterColumnPicker && a.screen != screenTableFilterManager && a.screen != screenTableSortColumnPicker && a.screen != screenTableSortDirectionPicker && a.screen != screenTableSortManager {
			return a.openResourceFinder()
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
	case screenLogs:
		return a.updateLogKeys(msg)
	case screenCommands:
		return a.updateCommandKeys(msg)
	case screenResourceFinder:
		return a.updateResourceFinderKeys(msg)
	case screenScopePicker:
		return a.updateScopePickerKeys(msg)
	case screenActionPicker:
		return a.updateActionPickerKeys(msg)
	case screenConfirmAction:
		return a.updateConfirmActionKeys(msg)
	case screenTableFilterColumnPicker:
		return a.updateTableFilterColumnPickerKeys(msg)
	case screenTableFilterManager:
		return a.updateTableFilterManagerKeys(msg)
	case screenTableSortColumnPicker:
		return a.updateTableSortColumnPickerKeys(msg)
	case screenTableSortDirectionPicker:
		return a.updateTableSortDirectionPickerKeys(msg)
	case screenTableSortManager:
		return a.updateTableSortManagerKeys(msg)
	}

	return nil
}

func (a *App) updateContextKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		a.navTable.MoveDown(1)
	case "J":
		a.navTable.MoveBottom()
	case "k", "up":
		a.navTable.MoveUp(1)
	case "K":
		a.navTable.MoveTop()
	case "home":
		a.navTable.MoveTop()
	case "end":
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
			a.persistSelectedContexts()
			a.refreshContextRows()
		}
	case "esc":
		if a.contextQuery != "" {
			a.contextQuery = ""
			a.refreshContextRows()
			return nil
		}
		a.toggleAllSelectedContexts()
		a.persistSelectedContexts()
		a.refreshContextRows()
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
	case "home":
		a.navTable.MoveTop()
	case "end":
		a.navTable.MoveBottom()
	case "enter":
		index := a.navTable.SelectedIndex()
		if index >= 0 && index < len(a.visibleGroups) {
			a.activeGroup = a.visibleGroups[index]
			a.screen = screenGroupResources
			a.resourceQuery = ""
			a.refreshGroupResources()
		}
	case "r":
		return a.openResourceFinder()
	case "a":
		a.namespace = ""
		a.refreshCatalog()
	case "n":
		return a.openNamespacePicker()
	case "c":
		return a.openContextScopePicker()
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
	case "home":
		a.navTable.MoveTop()
	case "end":
		a.navTable.MoveBottom()
	case "enter":
		index := a.navTable.SelectedIndex()
		if index >= 0 && index < len(a.visibleResources) {
			a.openResourceList(a.visibleResources[index])
			return a.maybeRefreshResourceUsageCmd(time.Now())
		}
	case "+":
		if resource, ok := a.selectedGroupResource(); ok {
			a.addFavoriteResource(resource)
			a.refreshCatalog()
			a.syncActiveGroupAfterFavoriteChange()
			a.refreshGroupResources()
		}
	case "-":
		if resource, ok := a.selectedGroupResource(); ok {
			a.removeFavoriteResource(resource)
			a.refreshCatalog()
			a.syncActiveGroupAfterFavoriteChange()
			if a.screen == screenCatalog {
				return nil
			}
			a.refreshGroupResources()
		}
	case "r":
		return a.openResourceFinder()
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
	case "J":
		a.podTable.MoveBottom()
	case "k", "up":
		a.podTable.MoveUp(1)
	case "K":
		a.podTable.MoveTop()
	case "g":
		if cmd, handled := a.tryOpenOwnerJump(time.Now()); handled {
			return cmd
		}
		a.statusMessage = "no owner targets"
		return nil
	case "home":
		a.podTable.MoveTop()
	case "G":
		if cmd, handled := a.tryOpenChildJump(time.Now()); handled {
			return cmd
		}
		a.statusMessage = "no dependent targets"
		return nil
	case "end":
		a.podTable.MoveBottom()
	case "pgdown":
		a.podTable.MoveDown(max(1, a.listPageSize()))
	case "pgup", "b":
		a.podTable.MoveUp(max(1, a.listPageSize()))
	case "h":
		a.podTable.MoveLeft(1)
	case "H", "shift+h":
		a.podTable.MoveColumnStart()
	case "l":
		a.podTable.MoveRight(1)
	case "L", "shift+l":
		a.podTable.MoveColumnEnd()
	case "+", "=":
		a.adjustSelectedTableColumnWidth(1)
	case "-", "_":
		a.adjustSelectedTableColumnWidth(-1)
	case "0":
		a.toggleSelectedTableColumnCollapse()
	case "tab":
		a.advanceNamespace(1)
	case "shift+tab":
		a.advanceNamespace(-1)
	case "a":
		a.namespace = ""
		a.refreshPods(time.Now())
	case "n":
		return a.openNamespacePicker()
	case "c":
		return a.openContextScopePicker()
	case "r":
		return a.openResourceFinder()
	case "f":
		return a.openTableFilterColumnPicker()
	case "F":
		return a.openTableFilterManager()
	case "y":
		return a.yankSelectedTable()
	case "Y":
		return a.yankFullTableRow()
	case "d":
		return a.deleteSelectedPod(time.Now())
	case "o":
		return a.openTableSortColumnPicker()
	case "O", "shift+o":
		return a.openTableSortManager()
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
		a.activePodUsage = a.podUsageByKey[details.Row.Key.String()]
		a.podUsageFetchedAt = time.Time{}
		a.podUsageLoading = false
		a.lastDataVersion = a.store.PodVersion()
		a.lastTick = time.Now()
		a.podDetailReturnScreen = screenPods
		a.screen = screenPodDetails
		a.resetTextViewport()
		return a.maybeRefreshResourceUsageCmd(time.Now())
	case "esc", "backspace":
		if a.podQuery != "" {
			a.podQuery = ""
			a.refreshPods(time.Now())
			return nil
		}
		a.backToResourceOrigin()
	}
	return nil
}

func (a *App) updateResourceListKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		a.resourceTable.MoveDown(1)
	case "J":
		a.resourceTable.MoveBottom()
	case "k", "up":
		a.resourceTable.MoveUp(1)
	case "K":
		a.resourceTable.MoveTop()
	case "g":
		if cmd, handled := a.tryOpenOwnerJump(time.Now()); handled {
			return cmd
		}
		a.statusMessage = "no owner targets"
		return nil
	case "home":
		a.resourceTable.MoveTop()
	case "G":
		if cmd, handled := a.tryOpenChildJump(time.Now()); handled {
			return cmd
		}
		a.statusMessage = "no dependent targets"
		return nil
	case "end":
		a.resourceTable.MoveBottom()
	case "pgdown":
		a.resourceTable.MoveDown(max(1, a.listPageSize()))
	case "pgup", "b":
		a.resourceTable.MoveUp(max(1, a.listPageSize()))
	case "h":
		a.resourceTable.MoveLeft(1)
	case "H", "shift+h":
		a.resourceTable.MoveColumnStart()
	case "l":
		a.resourceTable.MoveRight(1)
	case "L", "shift+l":
		a.resourceTable.MoveColumnEnd()
	case "+", "=":
		a.adjustSelectedTableColumnWidth(1)
	case "-", "_":
		a.adjustSelectedTableColumnWidth(-1)
	case "0":
		a.toggleSelectedTableColumnCollapse()
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
	case "n":
		return a.openNamespacePicker()
	case "c":
		return a.openContextScopePicker()
	case "r":
		return a.openResourceFinder()
	case "f":
		return a.openTableFilterColumnPicker()
	case "F":
		return a.openTableFilterManager()
	case "y":
		return a.yankSelectedTable()
	case "Y":
		return a.yankFullTableRow()
	case "d":
		return a.deleteSelectedResource(time.Now())
	case "o":
		return a.openTableSortColumnPicker()
	case "O", "shift+o":
		return a.openTableSortManager()
	case "P":
		if a.activeResource.Resource == "services" && a.activeResource.APIGroup == "" {
			if !a.selectCurrentResourceActionTarget(time.Now()) {
				a.statusMessage = "resource vanished during refresh"
				return nil
			}
			return a.runPortForwardResource()
		}
	case "S":
		if a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps" {
			if !a.selectCurrentResourceActionTarget(time.Now()) {
				a.statusMessage = "resource vanished during refresh"
				return nil
			}
			return a.openScalePrompt()
		}
	case "R":
		if a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps" {
			if !a.selectCurrentResourceActionTarget(time.Now()) {
				a.statusMessage = "resource vanished during refresh"
				return nil
			}
			return a.runRestartResource()
		}
	case "enter":
		if !a.openCurrentResourceSelection(a.resourceTable.SelectedIndex(), time.Now()) {
			a.statusMessage = "resource vanished during refresh"
			return nil
		}
		return a.maybeRefreshResourceUsageCmd(time.Now())
	case "esc", "backspace":
		if a.resourceQuery2 != "" {
			a.resourceQuery2 = ""
			a.refreshResourceList(time.Now())
			return nil
		}
		a.backToResourceOrigin()
	}
	return nil
}

func (a *App) updatePodDetailKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "g":
		if cmd, handled := a.tryOpenOwnerJump(time.Now()); handled {
			return cmd
		}
		a.statusMessage = "no owner targets"
		return nil
	case "G":
		if cmd, handled := a.tryOpenChildJump(time.Now()); handled {
			return cmd
		}
		a.statusMessage = "no dependent targets"
		return nil
	}
	if a.updateTextViewportKeys(msg) {
		return nil
	}
	switch msg.String() {
	case "esc", "backspace":
		if a.podDetailReturnScreen == screenResourceDetails {
			a.screen = screenResourceDetails
			a.detailFocus = detailFocusPods
			a.resetTextViewport()
			return nil
		}
		a.screen = screenPods
		a.refreshPods(time.Now())
	case "x":
		return a.runExecPod()
	case "e":
		return a.runEditPod()
	case "d":
		return a.runDeletePod()
	case "p":
		return a.runPortForwardPod()
	case "l":
		return a.runOpenLogs()
	case "r":
		return a.openResourceFinder()
	case "n":
		return a.openNamespacePicker()
	case "c":
		return a.openContextScopePicker()
	}
	return nil
}

func (a *App) updateResourceDetailKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "g":
		if cmd, handled := a.tryOpenOwnerJump(time.Now()); handled {
			return cmd
		}
		a.statusMessage = "no owner targets"
		return nil
	case "G":
		if cmd, handled := a.tryOpenChildJump(time.Now()); handled {
			return cmd
		}
		a.statusMessage = "no dependent targets"
		return nil
	}
	if a.resourceDetailHasPodTable() {
		if a.detailFocus == detailFocusPods {
			if a.updateResourceDetailPodTableKeys(msg) {
				return nil
			}
		} else if a.updateResourceDetailViewportKeys(msg) {
			return nil
		}
	} else if a.updateTextViewportKeys(msg) {
		return nil
	}
	switch msg.String() {
	case "tab", "]":
		if a.resourceDetailHasPodTable() {
			a.detailFocus = detailFocusPods
			return nil
		}
	case "shift+tab", "[":
		if a.resourceDetailHasPodTable() {
			a.detailFocus = detailFocusContent
			return nil
		}
	case "esc", "backspace":
		if a.isResourceListKindImplemented() {
			a.screen = screenResourceList
			a.refreshResourceList(time.Now())
			return nil
		}
		a.screen = screenGroupResources
		a.refreshGroupResources()
	case "x":
		return a.runExecResource()
	case "p":
		return a.runPortForwardResource()
	case "e":
		return a.runEditResource()
	case "d":
		return a.runDeleteResource()
	case "s":
		return a.openScalePrompt()
	case "l":
		return a.runOpenLogs()
	case "R":
		return a.openResourceFinder()
	case "r":
		return a.runRestartResource()
	case "n":
		return a.openNamespacePicker()
	case "c":
		return a.openContextScopePicker()
	}
	return nil
}

func (a *App) updateResourceDetailViewportKeys(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "j", "down":
		a.textViewport.ScrollDown(1)
		return true
	case "k", "up":
		a.textViewport.ScrollUp(1)
		return true
	case "home":
		a.textViewport.GotoTop()
		return true
	case "end":
		a.textViewport.GotoBottom()
		return true
	case "pgdown", "f":
		a.textViewport.PageDown()
		return true
	case "pgup", "b":
		a.textViewport.PageUp()
		return true
	default:
		return false
	}
}

func (a *App) updateResourceDetailPodTableKeys(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "j", "down":
		a.detailPodTable.MoveDown(1)
		return true
	case "J":
		a.detailPodTable.MoveBottom()
		return true
	case "k", "up":
		a.detailPodTable.MoveUp(1)
		return true
	case "K":
		a.detailPodTable.MoveTop()
		return true
	case "home":
		a.detailPodTable.MoveTop()
		return true
	case "end":
		a.detailPodTable.MoveBottom()
		return true
	case "pgdown", "f":
		a.detailPodTable.MoveDown(max(1, a.listPageSize()))
		return true
	case "pgup", "b":
		a.detailPodTable.MoveUp(max(1, a.listPageSize()))
		return true
	case "h":
		a.detailPodTable.MoveLeft(1)
		return true
	case "H", "shift+h":
		a.detailPodTable.MoveColumnStart()
		return true
	case "l":
		a.detailPodTable.MoveRight(1)
		return true
	case "L", "shift+l":
		a.detailPodTable.MoveColumnEnd()
		return true
	case "+", "=":
		a.adjustSelectedTableColumnWidth(1)
		return true
	case "-", "_":
		a.adjustSelectedTableColumnWidth(-1)
		return true
	case "0":
		a.toggleSelectedTableColumnCollapse()
		return true
	case "enter":
		return a.openSelectedDetailPod(time.Now())
	default:
		return false
	}
}

func (a *App) openSelectedDetailPod(now time.Time) bool {
	rows := a.currentAssociatedPodRows()
	index := a.detailPodTable.SelectedIndex()
	if index < 0 || index >= len(rows) {
		a.statusMessage = "pod vanished during refresh"
		return true
	}
	details, ok := a.store.PodDetailsByKey(rows[index].Key, now)
	if !ok {
		a.statusMessage = "pod vanished during refresh"
		return true
	}
	a.activePod = details
	a.activePodUsage = a.podUsageByKey[details.Row.Key.String()]
	a.podUsageFetchedAt = time.Time{}
	a.podUsageLoading = false
	a.lastDataVersion = a.store.PodVersion()
	a.lastTick = now
	a.podDetailReturnScreen = screenResourceDetails
	a.screen = screenPodDetails
	a.resetTextViewport()
	return true
}

func (a *App) updateCommandKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		a.navTable.MoveDown(1)
	case "k", "up":
		a.navTable.MoveUp(1)
	case "home":
		a.navTable.MoveTop()
	case "end":
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
		return "surfsk8s · resource catalog", a.navTable.View(), "r resource-find  n ns-find  c ctx-find  a all  enter open group  : commands  / filter  esc clear/back"
	case screenGroupResources:
		return "surfsk8s · " + a.activeGroup.Name, a.navTable.View(), "+ fav  - unfav  r resource-find  enter open resource  : commands  / filter  esc clear/back"
	case screenPods:
		return "", a.podTable.View(), a.podListFooter()
	case screenResourceList:
		return "", a.resourceTable.View(), a.resourceListFooter()
	case screenPodDetails:
		return "surfsk8s · pod details", a.renderPodDetails(), "j/k scroll  pgup/pgdn page  home/end edge  g owner  G dependents  r resource-find  n ns-find  c ctx-find  x exec  e edit  d delete  p port-forward  l logs  esc back"
	case screenResourceDetails:
		return a.resourceDetailTitle(), a.renderResourceDetails(), a.resourceDetailFooter()
	case screenLogs:
		return "surfsk8s · " + a.logTitle, a.renderLogs(), a.logFooter()
	case screenCommands:
		return "surfsk8s · commands", a.navTable.View(), "type to filter  j/k move  home/end edge  enter run  esc clear/close"
	case screenResourceFinder:
		return "surfsk8s · resource finder", a.navTable.View(), "+ fav  - unfav  type to filter  j/k move  home/end edge  enter open  esc close"
	case screenScopePicker:
		title := "namespace scope"
		if a.scopePickerKind == scopePickerContext {
			title = "context scope"
		}
		return "surfsk8s · " + title, a.navTable.View(), "type to filter  j/k move  home/end edge  enter select  esc cancel"
	case screenActionPicker:
		return "surfsk8s · " + a.actionPickerTitle, a.navTable.View(), a.actionPickerFooter
	case screenConfirmAction:
		return "surfsk8s · confirm action", a.renderConfirmAction(), "j/k scroll  pgup/pgdn page  home/end edge  enter confirm  esc cancel"
	case screenTableFilterColumnPicker:
		if a.filter.Active() && a.inputMode == inputModeTableFilterValue {
			return "surfsk8s · add filter", a.navTable.View(), "type filter  enter add  esc cancel"
		}
		return "surfsk8s · add filter", a.navTable.View(), "type fuzzy-find  enter select-column  esc cancel"
	case screenTableFilterManager:
		return "surfsk8s · filters", a.navTable.View(), "j/k move  home/end edge  enter/space toggle  x remove  esc close"
	case screenTableSortColumnPicker:
		return "surfsk8s · add sort", a.navTable.View(), "type fuzzy-find  enter select-column  esc cancel"
	case screenTableSortDirectionPicker:
		return "surfsk8s · sort direction", a.navTable.View(), "a asc  d desc  esc cancel"
	case screenTableSortManager:
		return "surfsk8s · sorts", a.navTable.View(), "j/k move  J/K reorder  enter/space toggle  x remove  esc close"
	default:
		return "surfsk8s", "", ""
	}
}

func (a *App) openFilter() {
	prompt := "/"
	placeholder := "filter (* ? =exact)"
	if a.screen == screenLogs {
		a.inputMode = inputModeSearch
		a.logFilterQuery = ""
		a.filter.SetPrompt("/")
		a.filter.SetPlaceholder("fuzzy log filter")
		a.filter.SetValue("")
		a.filter.Activate()
		return
	}
	if a.screen == screenCommands {
		prompt = ":"
		placeholder = "command"
		a.inputMode = inputModeCommand
		a.commandQuery = ""
		a.refreshCommands()
	} else {
		a.inputMode = inputModeSearch
		a.setCurrentQuery("")
		a.refreshCurrentScreen(time.Now())
	}

	a.filter.SetPrompt(prompt)
	a.filter.SetPlaceholder(placeholder)
	a.filter.SetValue("")
	a.filter.Activate()
}

func (a *App) openCommands() {
	a.prevScreen = a.screen
	a.screen = screenCommands
	a.inputMode = inputModeCommand
	a.commandQuery = ""
	a.filter.SetPrompt(":")
	a.filter.SetPlaceholder("command")
	a.filter.SetValue("")
	a.filter.Activate()
	a.refreshCommands()
}

func (a *App) openContextPicker(mode pickerMode) {
	a.prevScreen = a.screen
	a.screen = screenContexts
	a.inputMode = inputModeSearch
	a.filter.Clear()
	a.filter.Deactivate()
	a.pickerMode = mode
	a.contextQuery = ""
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
	case a.supportsGenericResourceList(resource):
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
	case screenResourceFinder:
		a.refreshResourceFinder()
	case screenScopePicker:
		a.refreshScopePicker()
	case screenActionPicker:
		a.refreshActionPicker()
	case screenConfirmAction:
		return
	case screenLogs:
		return
	case screenTableFilterColumnPicker:
		a.refreshTableFilterColumnPicker()
	case screenTableFilterManager:
		a.refreshTableFilterManager()
	case screenTableSortColumnPicker:
		a.refreshTableSortColumnPicker()
	case screenTableSortDirectionPicker:
		a.refreshTableSortDirectionPicker()
	case screenTableSortManager:
		a.refreshTableSortManager()
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
	now := time.Now()
	a.lastDataVersion = a.store.Version()
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = now
	a.catalog = applyFavoriteResources(a.manager.Catalog(), a.favoriteResourceIDs)
	if a.catalogOverviewScopeKey != a.catalogOverviewCurrentScopeKey() {
		a.catalogOverview = catalogOverviewData{ScopeLabel: a.catalogOverviewScopeLabel()}
	}
	groups := fuzzyGroups(a.catalog, a.catalogQuery)
	a.visibleGroups = groups
	a.visibleRows = len(groups)
	a.totalRows = len(a.catalog)
	a.setNavTable("GROUPS", renderGroupRows(groups))
	a.navTable.MoveTop()
}

func (a *App) refreshGroupResources() {
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = time.Now()
	resources := fuzzyResources(a.activeGroup.Resources, a.resourceQuery)
	a.visibleResources = resources
	a.visibleRows = len(resources)
	a.totalRows = len(a.activeGroup.Resources)
	a.setNavTable(strings.ToUpper(a.activeGroup.Name), renderResourceRows(resources, a.favoriteResources))
	a.navTable.MoveTop()
}

func (a *App) backToResourceOrigin() {
	if strings.TrimSpace(a.activeGroup.Name) == "" || len(a.activeGroup.Resources) == 0 {
		a.screen = screenCatalog
		a.refreshCatalog()
		return
	}
	a.screen = screenGroupResources
	a.refreshGroupResources()
}

func (a *App) namespaceListCacheKey(storeVersion uint64, listKind string) string {
	var b strings.Builder
	b.Grow(48)
	b.WriteString(strconv.FormatUint(storeVersion, 10))
	b.WriteByte('|')
	b.WriteString(a.contextScope)
	b.WriteByte('|')
	b.WriteString(listKind)
	return b.String()
}

func (a *App) refreshPods(now time.Time) {
	total := 0
	filtered := 0
	storeVersion := a.store.PodVersion()
	nsKey := a.namespaceListCacheKey(storeVersion, "pods")
	if nsKey != a.podNamespacesCacheKey {
		a.namespaces = a.podNamespacesForScope()
		a.podNamespacesCacheKey = nsKey
	}
	if a.podNeedsMaterializedSort() {
		total, filtered = a.buildSortedPods()
	} else {
		a.sortedPods = a.sortedPods[:0]
		a.store.ForEachPod(func(row state.PodRow) bool {
			total++
			if !a.matchPodRowAt(row, now) {
				return true
			}
			filtered++
			return true
		})
	}

	a.lastDataVersion = storeVersion
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = now
	a.visibleRows = filtered
	a.totalRows = total
	a.podTable.SetColumns(a.currentPodColumns())
	a.podTable.SetEmptyMessage(a.emptyMessageFor("pods"))
	a.podTable.SetWindowProvider(filtered, func(start int, end int) [][]string {
		return a.podTableRows(a.podWindow(start, end-start, time.Now()), time.Now())
	})
}

func (a *App) refreshResourceList(now time.Time) {
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = now
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		storeVersion := a.store.DeploymentVersion()
		a.lastDataVersion = storeVersion
		nsKey := a.namespaceListCacheKey(storeVersion, "deployments")
		if nsKey != a.deploymentNamespacesCacheKey {
			a.namespaces = a.deploymentNamespacesForScope()
			a.deploymentNamespacesCacheKey = nsKey
		}
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
		a.resourceTable.SetColumns(a.currentResourceColumns())
		a.resourceTable.SetEmptyMessage(a.emptyMessageFor("deployments"))
		a.resourceTable.SetWindowProvider(filtered, func(start int, end int) [][]string {
			return a.deploymentTableRows(a.deploymentWindow(start, end-start, time.Now()), time.Now())
		})
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		storeVersion := a.store.ServiceVersion()
		a.lastDataVersion = storeVersion
		nsKey := a.namespaceListCacheKey(storeVersion, "services")
		if nsKey != a.serviceNamespacesCacheKey {
			a.namespaces = a.serviceNamespacesForScope()
			a.serviceNamespacesCacheKey = nsKey
		}
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
		a.resourceTable.SetColumns(a.currentResourceColumns())
		a.resourceTable.SetEmptyMessage(a.emptyMessageFor("services"))
		a.resourceTable.SetWindowProvider(filtered, func(start int, end int) [][]string {
			return a.serviceTableRows(a.serviceWindow(start, end-start, time.Now()), time.Now())
		})
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		storeVersion := a.store.NodeVersion()
		a.lastDataVersion = storeVersion
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
		a.resourceTable.SetColumns(a.currentResourceColumns())
		a.resourceTable.SetEmptyMessage(a.emptyMessageFor("nodes"))
		a.resourceTable.SetWindowProvider(filtered, func(start int, end int) [][]string {
			return a.nodeTableRows(a.nodeWindow(start, end-start, time.Now()), time.Now())
		})
	default:
		a.refreshGenericResourceList(now)
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
	a.navTable.MoveTop()
}

func (a *App) refreshActivePodDetails(now time.Time) {
	storeVersion := a.store.PodVersion()
	if storeVersion == a.lastDataVersion {
		a.activePod.Row = a.activePod.Row.WithAge(now)
		a.lastTick = now
		return
	}
	resourceVersion, ok := a.store.PodResourceVersionByKey(a.activePod.Row.Key)
	if !ok {
		a.activePod = state.PodDetails{}
		a.activePodUsage = cluster.PodResourceUsage{}
		a.podUsageFetchedAt = time.Time{}
		a.podUsageLoading = false
		a.lastDataVersion = storeVersion
		a.lastManagerVersion = a.manager.Version()
		a.lastTick = now
		return
	}
	if resourceVersion != a.activePod.Row.ResourceVersion {
		updated, ok := a.store.PodDetailsByKey(a.activePod.Row.Key, now)
		if ok {
			a.activePod = updated
		}
	} else {
		a.activePod.Row = a.activePod.Row.WithAge(now)
	}
	a.lastDataVersion = storeVersion
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = now
}

func (a *App) refreshActiveResourceDetails(now time.Time) {
	if isBuiltInResourceList(a.activeResource) {
		storeVersion := a.builtInResourceDataVersion()
		if storeVersion == a.lastDataVersion {
			a.refreshCurrentDetailAge(now)
			a.refreshAssociatedPods(now)
			a.lastTick = now
			return
		}
		if !a.refreshCurrentDetail(now) {
			a.statusMessage = "resource vanished during refresh"
		}
		a.lastDataVersion = storeVersion
		a.lastManagerVersion = a.manager.Version()
		a.lastTick = now
		return
	}
	if a.supportsGenericResourceList(a.activeResource) {
		a.refreshGenericResourceDetails(now)
	}
}

func (a *App) refreshCurrentDetail(now time.Time) bool {
	switch a.activeResource.Resource {
	case "deployments":
		row, ok := a.store.DeploymentDetailsByKey(a.activeDeployment.Row.Key, now)
		if !ok {
			a.activeDeploymentPods = nil
			return false
		}
		a.activeDeployment = row
		a.activeDeploymentPods = a.deploymentAssociatedPods(now)
		return true
	case "services":
		row, ok := a.store.ServiceDetailsByKey(a.activeService.Row.Key, now)
		if !ok {
			a.activeServicePods = nil
			return false
		}
		a.activeService = row
		a.activeServicePods = a.serviceAssociatedPods(now)
		return true
	case "nodes":
		row, ok := a.store.NodeDetailsByKey(a.activeNode.Row.Key, now)
		if !ok {
			a.activeNodeUsage = cluster.NodeResourceUsage{}
			a.nodeUsageFetchedAt = time.Time{}
			a.nodeUsageLoading = false
			a.activeNodePods = nil
			return false
		}
		a.activeNode = row
		a.activeNodePods = a.nodeAssociatedPods(now)
		return true
	default:
		return false
	}
}

func (a *App) refreshCurrentDetailAge(now time.Time) {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		a.activeDeployment.Row = a.activeDeployment.Row.WithAge(now)
		for idx := range a.activeDeploymentPods {
			a.activeDeploymentPods[idx] = a.activeDeploymentPods[idx].WithAge(now)
		}
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		a.activeService.Row = a.activeService.Row.WithAge(now)
		for idx := range a.activeServicePods {
			a.activeServicePods[idx] = a.activeServicePods[idx].WithAge(now)
		}
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		a.activeNode.Row = a.activeNode.Row.WithAge(now)
		for idx := range a.activeNodePods {
			a.activeNodePods[idx] = a.activeNodePods[idx].WithAge(now)
		}
	}
}

func (a *App) openCurrentResourceSelection(index int, now time.Time) bool {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		row, ok := a.deploymentRowAt(index, now)
		if !ok {
			return false
		}
		details, ok := a.store.DeploymentDetailsByKey(row.Key, now)
		if !ok {
			return false
		}
		a.activeDeployment = details
		a.activeDeploymentPods = a.deploymentAssociatedPods(now)
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		row, ok := a.serviceRowAt(index, now)
		if !ok {
			return false
		}
		details, ok := a.store.ServiceDetailsByKey(row.Key, now)
		if !ok {
			return false
		}
		a.activeService = details
		a.activeServicePods = a.serviceAssociatedPods(now)
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		row, ok := a.nodeRowAt(index, now)
		if !ok {
			return false
		}
		details, ok := a.store.NodeDetailsByKey(row.Key, now)
		if !ok {
			return false
		}
		a.activeNode = details
		a.activeNodePods = a.nodeAssociatedPods(now)
		a.activeNodeUsage = a.nodeUsageByKey[details.Row.Key.String()]
		a.nodeUsageFetchedAt = time.Time{}
		a.nodeUsageLoading = false
	default:
		return a.openCurrentGenericResourceSelection(index, now)
	}
	a.screen = screenResourceDetails
	a.detailFocus = detailFocusContent
	a.lastDataVersion = a.builtInResourceDataVersion()
	a.lastTick = now
	a.resetTextViewport()
	return true
}

func (a *App) setNavTable(title string, rows [][]string) {
	width := max(24, a.width-4)
	a.navTable.SetColumns([]components.Column{{Title: title, Width: width}})
	a.navTable.SetRows(rows)
	a.navTable.SetSize(width, max(4, a.height-2))
}

func (a *App) bodyHeight(topRows int) int {
	return max(4, a.height-topRows)
}

func (a *App) resizeTablesForBody(topRows int) {
	bodyHeight := a.bodyHeight(topRows)
	contentWidth := max(24, a.width-2)
	a.navTable.SetSize(max(24, a.width-4), bodyHeight)
	a.podTable.SetSize(contentWidth, bodyHeight)
	a.resourceTable.SetSize(contentWidth, bodyHeight)
}

func (a *App) resizeTables() {
	a.resizeTablesForBody(2)
}

func (a *App) renderPodDetails() string {
	return renderPodDetails(a.resourceDetailContentWidth(), a.activePod, a.activePodUsage, formatUsageDetailLabel(time.Now(), a.podUsageFetchedAt, a.podUsageLoading))
}

func (a *App) resourceDetailContentWidth() int {
	if a.width <= 0 {
		return 120
	}
	if a.screen == screenResourceDetails && a.resourceDetailHasPodTable() {
		paneOuterWidth := max(24, a.width-2)
		return max(20, paneOuterWidth-2)
	}
	return max(20, a.width-2)
}

func (a *App) renderResourceDetails() string {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		return a.renderDeploymentDetails()
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		return a.renderServiceDetails()
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		return a.renderNodeDetails()
	default:
		return a.renderGenericResourceDetails()
	}
}

func (a *App) podListFooter() string {
	return "hjkl nav  HJKL jump  g owner  G dependents  +/- width  0 collapse  enter open  d delete  y row  Y CSV  / filter  f add-filter  F filters  o add-sort  O sorts  r resource-find  n ns-find  c ctx-find  esc back"
}

func (a *App) resourceListFooter() string {
	base := "hjkl nav  HJKL jump  g owner  G dependents  +/- width  0 collapse  enter open  d delete  y row  Y CSV  / filter  f add-filter  F filters  o add-sort  O sorts  r resource-find  c ctx-find  "
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		return base + "n ns-find  S scale  R restart  esc back"
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		return base + "n ns-find  P port-forward  esc back"
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		return base + "esc back"
	default:
		if a.activeResource.Namespaced {
			return base + "n ns-find  esc back"
		}
		return base + "esc back"
	}
}

func (a *App) resourceDetailTitle() string {
	if !a.resourceDetailHasPodTable() {
		return "surfsk8s · resource details"
	}
	pane := "details"
	if a.detailFocus == detailFocusPods {
		pane = "pods"
	}
	return "surfsk8s · resource details · active pane: " + pane
}

func (a *App) resourceDetailFooter() string {
	prefix := "j/k scroll  pgup/pgdn page  home/end edge  g owner  G dependents  [/tab pane-up  ]/tab pane-down  R resource-find  c ctx-find  "
	podTableHints := "enter open-pod  +/- width  0 collapse  "
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		return prefix + podTableHints + "n ns-find  s scale  r restart  e edit  d delete  l logs  esc back"
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		return prefix + podTableHints + "n ns-find  p port-forward  e edit  d delete  esc back"
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		return prefix + podTableHints + "x shell  e edit  d delete  l logs  esc back"
	default:
		if a.activeResource.Namespaced {
			return prefix + "n ns-find  e edit  d delete  esc back"
		}
		return prefix + "e edit  d delete  esc back"
	}
}

func (a *App) builtInResourceDataVersion() uint64 {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		return a.store.DeploymentVersion()
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		return a.store.ServiceVersion()
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		return a.store.NodeVersion()
	default:
		return a.store.Version()
	}
}

func (a *App) shouldRefresh(now time.Time) bool {
	switch a.screen {
	case screenCatalog:
		return a.store.Version() != a.lastDataVersion || a.manager.Version() != a.lastManagerVersion
	case screenPods:
		return a.store.PodVersion() != a.lastDataVersion
	case screenResourceList:
		if isBuiltInResourceList(a.activeResource) {
			return a.builtInResourceDataVersion() != a.lastDataVersion
		}
		return a.genericResourceVersion() != a.lastManagerVersion
	case screenPodDetails:
		return a.store.PodVersion() != a.lastDataVersion || now.Sub(a.lastTick) >= time.Second
	case screenResourceDetails:
		if isBuiltInResourceList(a.activeResource) {
			return a.builtInResourceDataVersion() != a.lastDataVersion || now.Sub(a.lastTick) >= time.Second
		}
		return a.genericResourceVersion() != a.lastManagerVersion || now.Sub(a.lastTick) >= time.Second
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

func (a *App) isFavoriteResource(resource cluster.ResourceKind) bool {
	return a.favoriteResources[resource.ID]
}

func (a *App) addFavoriteResource(resource cluster.ResourceKind) {
	if a.favoriteResources[resource.ID] {
		a.statusMessage = resource.Display + " already in favourites"
		return
	}
	a.favoriteResourceIDs = append(a.favoriteResourceIDs, resource.ID)
	if a.favoriteResources == nil {
		a.favoriteResources = make(map[string]bool, 8)
	}
	a.favoriteResources[resource.ID] = true
	a.persistFavoriteResources()
	a.statusMessage = resource.Display + " added to favourites"
}

func (a *App) removeFavoriteResource(resource cluster.ResourceKind) {
	if !a.favoriteResources[resource.ID] {
		a.statusMessage = resource.Display + " not in favourites"
		return
	}
	delete(a.favoriteResources, resource.ID)
	filtered := a.favoriteResourceIDs[:0]
	for _, id := range a.favoriteResourceIDs {
		if id == resource.ID {
			continue
		}
		filtered = append(filtered, id)
	}
	a.favoriteResourceIDs = filtered
	a.persistFavoriteResources()
	a.statusMessage = resource.Display + " removed from favourites"
}

func (a *App) selectedGroupResource() (cluster.ResourceKind, bool) {
	index := a.navTable.SelectedIndex()
	if index < 0 || index >= len(a.visibleResources) {
		return cluster.ResourceKind{}, false
	}
	return a.visibleResources[index], true
}

func (a *App) syncActiveGroupAfterFavoriteChange() {
	for _, group := range a.catalog {
		if group.Name != a.activeGroup.Name {
			continue
		}
		a.activeGroup = group
		return
	}
	if a.activeGroup.Name == "Favourites" {
		a.screen = screenCatalog
	}
}

func applyFavoriteResources(catalog []cluster.ResourceGroup, favoriteResourceIDs []string) []cluster.ResourceGroup {
	resourceByID := make(map[string]cluster.ResourceKind, 64)
	groups := make([]cluster.ResourceGroup, 0, len(catalog)+1)
	for _, group := range catalog {
		if group.Name == "Favourites" {
			continue
		}
		groups = append(groups, group)
		for _, resource := range group.Resources {
			if _, ok := resourceByID[resource.ID]; ok {
				continue
			}
			resourceByID[resource.ID] = resource
		}
	}
	favorites := make([]cluster.ResourceKind, 0, len(favoriteResourceIDs))
	for _, id := range favoriteResourceIDs {
		resource, ok := resourceByID[id]
		if !ok {
			continue
		}
		resource.GroupName = "Favourites"
		resource.Favorite = true
		favorites = append(favorites, resource)
	}
	if len(favorites) == 0 {
		return groups
	}
	result := make([]cluster.ResourceGroup, 0, len(groups)+1)
	result = append(result, cluster.ResourceGroup{Name: "Favourites", Resources: favorites})
	result = append(result, groups...)
	return result
}

func (a *App) toggleAllSelectedContexts() {
	if len(a.selectedContext) == 0 {
		for _, context := range a.contexts {
			a.selectedContext[context.Name] = true
		}
		return
	}
	clear(a.selectedContext)
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
	case screenResourceFinder:
		return a.resourceFinderQuery
	case screenScopePicker:
		return a.scopeQuery
	case screenTableFilterColumnPicker:
		return a.tableFilterColumnQuery
	case screenTableSortColumnPicker:
		return a.tableSortColumnQuery
	case screenLogs:
		return a.logFilterQuery
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
	case screenResourceFinder:
		a.resourceFinderQuery = value
	case screenScopePicker:
		a.scopeQuery = value
	case screenTableFilterColumnPicker:
		a.tableFilterColumnQuery = value
	case screenTableSortColumnPicker:
		a.tableSortColumnQuery = value
	case screenLogs:
		a.logFilterQuery = value
	}
}

func (a *App) currentContextLabel() string {
	if a.contextScope != "" {
		return a.contextScope
	}
	connected := a.manager.ConnectedContextNames()
	if len(connected) == 0 {
		return "all"
	}
	return fmt.Sprintf("all(%d)", len(connected))
}

func (a *App) currentNamespaceLabel() string {
	if (a.screen == screenResourceList || a.screen == screenResourceDetails) && !a.activeResource.Namespaced {
		return "cluster"
	}
	return a.namespace
}

func (a *App) contextMatches(clusterName string) bool {
	if a.contextScope == "" {
		return true
	}
	return clusterName == a.contextScope
}

func collectNamespaces[T any](appendRows func(func(string) bool)) []string {
	seen := make(map[string]struct{}, 16)
	namespaces := make([]string, 0, 16)
	appendRows(func(namespace string) bool {
		if namespace == "" {
			return true
		}
		if _, ok := seen[namespace]; ok {
			return true
		}
		seen[namespace] = struct{}{}
		namespaces = append(namespaces, namespace)
		return true
	})
	sort.Strings(namespaces)
	if len(namespaces) == 0 {
		return []string{""}
	}
	return namespaces
}

func (a *App) podNamespacesForScope() []string {
	return collectNamespaces[struct{}](func(appendNamespace func(string) bool) {
		a.store.ForEachPod(func(row state.PodRow) bool {
			if !a.contextMatches(row.Cluster) {
				return true
			}
			return appendNamespace(row.Namespace)
		})
	})
}

func (a *App) deploymentNamespacesForScope() []string {
	return collectNamespaces[struct{}](func(appendNamespace func(string) bool) {
		a.store.ForEachDeployment(func(row state.DeploymentRow) bool {
			if !a.contextMatches(row.Cluster) {
				return true
			}
			return appendNamespace(row.Namespace)
		})
	})
}

func (a *App) serviceNamespacesForScope() []string {
	return collectNamespaces[struct{}](func(appendNamespace func(string) bool) {
		a.store.ForEachService(func(row state.ServiceRow) bool {
			if !a.contextMatches(row.Cluster) {
				return true
			}
			return appendNamespace(row.Namespace)
		})
	})
}

func (a *App) isResourceListKindImplemented() bool {
	return isBuiltInResourceList(a.activeResource) || a.supportsGenericResourceList(a.activeResource)
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

func renderResourceRows(resources []cluster.ResourceKind, favorites map[string]bool) [][]string {
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
		marker := "  "
		if favorites[resource.ID] {
			marker = "★ "
		}
		rows = append(rows, []string{fmt.Sprintf("%s%s  (%s · %s)", marker, prefix, apiGroup, scope)})
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
		score, ok := resourceSearchScore(resource, query, "")
		if !ok {
			continue
		}
		matched = append(matched, scoredResource{resource: resource, score: score})
	}
	sort.SliceStable(matched, func(i int, j int) bool {
		if matched[i].score != matched[j].score {
			return matched[i].score > matched[j].score
		}
		left := strings.ToLower(matched[i].resource.Display + " " + matched[i].resource.APIGroup)
		right := strings.ToLower(matched[j].resource.Display + " " + matched[j].resource.APIGroup)
		return left < right
	})
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

func resourceSearchScore(resource cluster.ResourceKind, query string, groupName string) (int, bool) {
	fields := []struct {
		value  string
		weight int
	}{
		{value: resource.Display, weight: 120},
		{value: resource.Kind, weight: 100},
		{value: resource.Resource, weight: 80},
		{value: resource.APIGroup, weight: 40},
		{value: groupName, weight: 20},
	}
	best := 0
	matched := false
	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" {
			continue
		}
		score, ok := scoreSearchCandidate(field.value, query)
		if !ok {
			continue
		}
		score += field.weight
		if !matched || score > best {
			best = score
		}
		matched = true
	}
	return best, matched
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
	return a.matchPodRowAt(row, time.Now())
}

func (a *App) matchPodRowAt(row state.PodRow, now time.Time) bool {
	if !a.contextMatches(row.Cluster) {
		return false
	}
	if a.namespace != "" && row.Namespace != a.namespace {
		return false
	}
	if !matchesSearch(row.SearchText(), a.podQuery) {
		return false
	}
	return a.matchesPodColumnFilters(row, now)
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
		if !a.matchPodRowAt(row, now) {
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
		if !a.contextMatches(row.Cluster) {
			return true
		}
		total++
		if a.namespace != "" && row.Namespace != a.namespace {
			return true
		}
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		if !a.matchesDeploymentColumnFilters(row, time.Now()) {
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
		if !a.contextMatches(row.Cluster) {
			return true
		}
		if a.namespace != "" && row.Namespace != a.namespace {
			return true
		}
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		if !a.matchesDeploymentColumnFilters(row, now) {
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
		if !a.contextMatches(row.Cluster) {
			return true
		}
		total++
		if a.namespace != "" && row.Namespace != a.namespace {
			return true
		}
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		if !a.matchesServiceColumnFilters(row, time.Now()) {
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
		if !a.contextMatches(row.Cluster) {
			return true
		}
		if a.namespace != "" && row.Namespace != a.namespace {
			return true
		}
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		if !a.matchesServiceColumnFilters(row, now) {
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
		if !a.contextMatches(row.Cluster) {
			return true
		}
		total++
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		if !a.matchesNodeColumnFilters(row, time.Now()) {
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
		if !a.contextMatches(row.Cluster) {
			return true
		}
		if !matchesSearch(row.SearchText(), a.resourceQuery2) {
			return true
		}
		if !a.matchesNodeColumnFilters(row, now) {
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
	a.clusterStatusBuf = a.manager.StatusesInto(a.clusterStatusBuf[:0])
	statuses := a.clusterStatusBuf
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
