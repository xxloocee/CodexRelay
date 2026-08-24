/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : 独立提醒窗口屏幕定位回归测试
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
package desktop

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestNotificationWindowMatchesVisibleHTMLCardBounds(t *testing.T) {
	width, height := notificationWindowSize()
	if width != 410 || height != 280 {
		t.Fatalf("notification window size = %dx%d, want 410x280", width, height)
	}
	workArea := application.Rect{X: 0, Y: 0, Width: 1920, Height: 1040}
	x, y := notificationWindowPosition(workArea, width, height, 0)
	if x != 1482 || y != 732 {
		t.Fatalf("first visible card position = (%d,%d), want (1482,732)", x, y)
	}
	nextX, nextY := notificationWindowPosition(workArea, width, height, 1)
	if nextX != x || nextY != 420 {
		t.Fatalf("second visible card position = (%d,%d), want (%d,420)", nextX, nextY, x)
	}
}

func TestNotificationWindowPositionWrapsAndStaysInsideWorkArea(t *testing.T) {
	tests := []application.Rect{
		{X: 0, Y: 0, Width: 1366, Height: 728},
		{X: 0, Y: 0, Width: 1920, Height: 1040},
		{X: -1920, Y: 0, Width: 1920, Height: 1040},
	}
	width, height := notificationWindowSize()
	for _, workArea := range tests {
		positions := make(map[[2]int]struct{})
		for index := 0; index < 12; index++ {
			x, y := notificationWindowPosition(workArea, width, height, index)
			if x < workArea.X || y < workArea.Y || x+width > workArea.X+workArea.Width || y+height > workArea.Y+workArea.Height {
				t.Fatalf("work area %+v index %d position (%d,%d) is outside screen", workArea, index, x, y)
			}
			positions[[2]int{x, y}] = struct{}{}
		}
		if len(positions) != 12 {
			t.Fatalf("work area %+v did not wrap visible windows into distinct positions: %+v", workArea, positions)
		}
	}
}
