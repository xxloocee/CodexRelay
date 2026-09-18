/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : 透明 ReverseProxy 请求处理
 */
package relay

import (
	"log"
	"net/http"
	"net/http/httputil"
	"time"

	"codexrelay/internal/config"
	"codexrelay/internal/usage"
)

func (r *Runtime) ProxyHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		state := r.state.Load()
		if state == nil {
			writeProxyError(writer, http.StatusServiceUnavailable, "代理尚未初始化")
			return
		}
		if !validLocalToken(request, state.Config.LocalAccessToken) {
			writeProxyError(writer, http.StatusUnauthorized, "本地访问令牌不正确")
			return
		}
		category, upstreamPath, ok := RoutePath(request.URL.Path)
		if !ok {
			writeProxyError(writer, http.StatusNotFound, "本地地址必须使用 /codex、/claude、/gemini、/grok、/opencode、/openclaw、/hermes、/image 或 /other 前缀")
			return
		}
		active := state.Active[category]
		if active == nil {
			writeProxyError(writer, http.StatusServiceUnavailable, "该 API 类别尚未启用代理 API")
			return
		}

		started := time.Now()
		active.LastRequestAt.Store(started.UnixMilli())
		requestID := r.nextID.Add(1)
		statusCode := http.StatusBadGateway
		proxyError := ""
		var observer *usage.Observer
		proxy := &httputil.ReverseProxy{
			FlushInterval: -1,
			Transport:     active.Transport,
			Director: func(out *http.Request) {
				out.URL.Scheme = active.Target.Scheme
				out.URL.Host = active.Target.Host
				out.URL.Path = JoinTargetPath(active.Target.Path, upstreamPath)
				out.URL.RawPath = ""
				out.URL.RawQuery = joinQuery(active.Target.RawQuery, request.URL.RawQuery)
				out.Host = active.Target.Host
				out.Header.Set("Authorization", "Bearer "+active.APIKey)
				out.Header.Del("Proxy-Authorization")
				for name, value := range active.Profile.Headers {
					out.Header.Set(name, value)
				}
			},
			ModifyResponse: func(response *http.Response) error {
				statusCode = response.StatusCode
				if category == config.CategoryImage || category == config.CategoryOther {
					return nil
				}
				observer = usage.NewObserver(response.Header)
				if observer.Enabled() && response.Body != nil {
					response.Body = observer.Wrap(response.Body)
				}
				return nil
			},
			ErrorHandler: func(responseWriter http.ResponseWriter, _ *http.Request, err error) {
				proxyError = SanitizeError(err)
				statusCode = http.StatusBadGateway
				writeProxyError(responseWriter, http.StatusBadGateway, "连接上游失败: "+proxyError)
			},
		}
		proxy.ServeHTTP(writer, request)
		observed := usage.Observed{Status: usage.StatusUnreported}
		if observer != nil {
			observed = observer.Finish()
		}
		if err := r.record(usage.RequestRecord{
			ID: requestID, StartedAt: started, Method: request.Method, Path: request.URL.RequestURI(),
			ProfileID: active.Profile.ID, Profile: active.Profile.Name, Status: statusCode,
			Duration: time.Since(started).Milliseconds(), Error: proxyError,
			UsageStatus: observed.Status, Model: observed.Model, InputTokens: observed.InputTokens,
			OutputTokens: observed.OutputTokens, CachedTokens: observed.CachedTokens, TotalTokens: observed.TotalTokens,
		}); err != nil {
			log.Printf("CodexRelay: 保存用量统计失败: %v", err)
		}
		r.ObserveUpstreamResult(active.Profile.ID, category, statusCode, proxyError != "")
	})
}
