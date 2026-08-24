/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 右下角公告、额度和令牌切换提醒窗口管理
 * @File          : 独立 Wails 提醒窗口与主窗口状态联动
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package desktop

import (
	"sync"

	"codexrelay/internal/config"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	// 原生窗口必须与白色 HTML 卡片同尺寸，不能为阴影保留透明命中区域，否则不可见区域会拦截桌面点击。
	notificationWindowWidth  = 410
	notificationWindowHeight = 280
	// 原窗口四周有 10px HTML 留白；窗口缩小后补偿屏幕边距和窗口间距，使白色卡片的位置保持不变。
	notificationWindowMargin = 28
	notificationWindowGap    = 32
)

var notificationKinds = []string{
	NotificationKindBalance,
	NotificationKindSubscription,
	NotificationKindAnnouncement,
}

func notificationWindowSize() (int, int) {
	return notificationWindowWidth, notificationWindowHeight
}

// notificationWindowManager 按提醒类别维护独立窗口；窗口生命周期和显示条件不依赖主窗口。
// mainWindow 仅用于读取当前屏幕位置，主窗口可见、最小化或隐藏都不会改变提醒窗口状态。
type notificationWindowManager struct {
	mu         sync.Mutex
	refreshMu  sync.Mutex
	wailsApp   *application.App
	mainWindow *application.WebviewWindow
	service    *DesktopService
	windows    map[string]*application.WebviewWindow
	ready      map[string]bool
}

func newNotificationWindowManager(wailsApp *application.App, mainWindow *application.WebviewWindow, service *DesktopService) *notificationWindowManager {
	manager := &notificationWindowManager{
		wailsApp:   wailsApp,
		mainWindow: mainWindow,
		service:    service,
		windows:    make(map[string]*application.WebviewWindow, len(notificationKinds)+len(config.Categories)),
		ready:      make(map[string]bool, len(notificationKinds)+len(config.Categories)),
	}
	for _, kind := range notificationKinds {
		manager.addWindow(kind, "/notification.html?kind="+kind)
	}
	for _, category := range config.Categories {
		key := tokenSwitchWindowKey(category)
		manager.addWindow(key, "/notification.html?kind="+NotificationKindTokenSwitch+"&category="+category)
	}
	return manager
}

func (m *notificationWindowManager) addWindow(key, windowURL string) {
	width, height := notificationWindowSize()
	window := m.wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "notification-" + key,
		Title:            "CodexRelay 提醒",
		Width:            width,
		Height:           height,
		MinWidth:         width,
		MinHeight:        height,
		MaxWidth:         width,
		MaxHeight:        height,
		URL:              windowURL,
		Frameless:        true,
		DisableResize:    true,
		AlwaysOnTop:      true,
		Hidden:           true,
		BackgroundColour: application.NewRGBA(255, 255, 255, 0),
	})
	m.windows[key] = window
	// 提醒窗口自身的运行时就绪才是提醒系统可刷新的可靠信号；主窗口可能按开机隐藏策略一直不显示。
	window.RegisterHook(events.Common.WindowRuntimeReady, func(_ *application.WindowEvent) {
		m.markStarted(key)
	})
}

func tokenSwitchWindowKey(category string) string {
	return NotificationKindTokenSwitch + "-" + category
}

// markStarted 标记提醒窗口运行时已经可用，并重放一次当前状态。
// 启动同步可能早于任一窗口就绪完成，因此就绪事件必须主动重放待显示的提醒。
func (m *notificationWindowManager) markStarted(key string) {
	m.mu.Lock()
	m.ready[key] = true
	m.mu.Unlock()
	m.refresh()
}

func (m *notificationWindowManager) refresh() {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	m.mu.Lock()
	ready := make(map[string]bool, len(m.ready))
	for key, value := range m.ready {
		ready[key] = value
	}
	m.mu.Unlock()
	if m.service == nil {
		return
	}
	state := m.service.GetState()
	alertsByKind := make(map[string][]PublicDogeAlert, len(notificationKinds)+len(state.Doge.TokenSwitches))
	for _, alert := range state.Doge.Notifications.Alerts {
		alertsByKind[alert.Kind] = append(alertsByKind[alert.Kind], alert)
	}
	for category, prompt := range state.Doge.TokenSwitches {
		if prompt != nil {
			key := tokenSwitchWindowKey(category)
			alertsByKind[key] = []PublicDogeAlert{{Kind: NotificationKindTokenSwitch, Key: prompt.Key}}
		}
	}
	screen := m.notificationScreen()
	keys := append([]string(nil), notificationKinds...)
	for _, category := range config.Categories {
		keys = append(keys, tokenSwitchWindowKey(category))
	}
	visibleIndex := 0
	for _, key := range keys {
		window := m.windows[key]
		if !ready[key] {
			continue
		}
		width, height := notificationWindowSize()
		alerts := alertsByKind[key]
		if len(alerts) == 0 {
			window.Hide()
			continue
		}
		if screen != nil {
			window.SetScreen(screen)
			x, y := notificationWindowPosition(screen.WorkArea, width, height, visibleIndex)
			window.SetPosition(x, y)
		}
		window.EmitEvent("notification-state-changed")
		// 同一低余额记录在后台同步后仍然存在，但不能再次调用 Show 把窗口当成新提醒反复弹出。
		// 窗口被用户关闭或此前没有显示时仍需 Show，状态变化只刷新已显示窗口的内容。
		if !window.IsVisible() {
			window.Show()
		}
		visibleIndex++
	}
}

func (m *notificationWindowManager) notificationScreen() *application.Screen {
	if m.mainWindow != nil {
		if screen, err := m.mainWindow.GetScreen(); err == nil && screen != nil {
			return screen
		}
	}
	if m.wailsApp == nil {
		return nil
	}
	if screen := m.wailsApp.Screen.GetPrimary(); screen != nil {
		return screen
	}
	screens := m.wailsApp.Screen.GetAll()
	if len(screens) > 0 {
		return screens[0]
	}
	return nil
}

// notificationWindowPosition 从右下角向上排布，放不下时向左换列，并将最终坐标限制在工作区内。
func notificationWindowPosition(workArea application.Rect, width, height, index int) (int, int) {
	usableHeight := workArea.Height - 2*notificationWindowMargin
	rows := (usableHeight + notificationWindowGap) / (height + notificationWindowGap)
	if rows < 1 {
		rows = 1
	}
	usableWidth := workArea.Width - 2*notificationWindowMargin
	columns := (usableWidth + notificationWindowGap) / (width + notificationWindowGap)
	if columns < 1 {
		columns = 1
	}
	capacity := rows * columns
	slot := index % capacity
	cycle := index / capacity
	row := slot % rows
	column := slot / rows
	cascade := cycle * 18
	x := workArea.X + workArea.Width - width - notificationWindowMargin - column*(width+notificationWindowGap) + cascade
	y := workArea.Y + workArea.Height - height - notificationWindowMargin - row*(height+notificationWindowGap) - cascade
	minX := workArea.X
	maxX := workArea.X + workArea.Width - width
	minY := workArea.Y
	maxY := workArea.Y + workArea.Height - height
	if x < minX {
		x = minX
	}
	if x > maxX {
		x = maxX
	}
	if y < minY {
		y = minY
	}
	if y > maxY {
		y = maxY
	}
	return x, y
}
