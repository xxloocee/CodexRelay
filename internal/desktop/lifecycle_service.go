package desktop

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func proxyListenAddress(port int, listenOnAllInterfaces bool) string {
	host := "127.0.0.1"
	if listenOnAllInterfaces {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func (s *DesktopService) prepareProxyListener(port int, listenOnAllInterfaces bool) (net.Listener, *http.Server, error) {
	address := proxyListenAddress(port, listenOnAllInterfaces)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, fmt.Errorf("代理端口 %s 无法监听，可能已有实例正在运行: %w", address, err)
	}
	server := &http.Server{
		Handler: s.runtime.ProxyHandler(), ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout: 120 * time.Second,
	}
	return listener, server, nil
}

func (s *DesktopService) serveProxy(server *http.Server, listener net.Listener) {
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			application.Get().Logger.Error("透明代理异常退出", "error", err)
		}
	}()
}

// installProxyListener 替换当前监听；调用方须先完成新监听绑定和配置持久化。
// 新监听启动后旧服务再优雅退出，避免改端口时出现不必要的代理空窗。
func (s *DesktopService) installProxyListener(server *http.Server, listener net.Listener) bool {
	s.mu.Lock()
	oldServer := s.server
	oldListener := s.listener
	if oldServer == nil && oldListener == nil {
		s.mu.Unlock()
		return false
	}
	s.server = server
	s.listener = listener
	s.mu.Unlock()
	s.serveProxy(server, listener)
	if oldServer != nil {
		// 不在配置事务锁内等待旧请求结束；请求完成后的健康回调可能还要
		// 进入故障切换并获取同一把锁。新监听已就绪，旧服务异步优雅退出。
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := oldServer.Shutdown(ctx); err != nil {
				application.Get().Logger.Error("旧监听端口关闭失败", "error", err)
			}
		}()
	} else if oldListener != nil {
		_ = oldListener.Close()
	}
	return true
}

func (s *DesktopService) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	state := s.runtime.State()
	if state == nil {
		return errors.New("代理尚未初始化")
	}
	// 启动时只探测各客户端的已知配置目录；延迟存储会在首次初始化完成后再落盘。
	if err := s.scanClientConfigs(); err != nil {
		application.Get().Logger.Warn("扫描外部客户端配置目录失败", "error", err)
	}
	listener, server, err := s.prepareProxyListener(state.Config.ProxyPort, state.Config.ListenOnAllInterfaces)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.listener = listener
	s.server = server
	s.mu.Unlock()
	s.serveProxy(server, listener)
	s.mu.Lock()
	if !s.taskNotifyStarted {
		s.taskNotifyStarted = true
		ctx, cancel := context.WithCancel(context.Background())
		s.taskNotifyCancel = cancel
		s.mu.Unlock()
		s.taskNotifier.Start(ctx)
	} else {
		s.mu.Unlock()
	}
	s.mu.Lock()
	if !s.syncStarted {
		s.syncStarted = true
		ctx, cancel := context.WithCancel(context.Background())
		s.syncCancel = cancel
		s.mu.Unlock()
		go s.dogeSyncLoop(ctx)
		if strings.TrimSpace(state.Config.Doge.AccessToken) != "" {
			// 启动恢复只刷新目录元数据；已有密钥来自本地缓存，新令牌才按需补全。
			go func() { _ = s.syncDoge(context.Background(), "", false, dogeSyncMetadata) }()
		} else {
			go func() { _ = s.SyncDogeAnnouncements() }()
		}
	} else {
		s.mu.Unlock()
	}
	return nil
}

func (s *DesktopService) ServiceShutdown() error {
	s.mu.Lock()
	server := s.server
	cancel := s.syncCancel
	taskNotifyCancel := s.taskNotifyCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if taskNotifyCancel != nil {
		taskNotifyCancel()
	}
	if server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}
