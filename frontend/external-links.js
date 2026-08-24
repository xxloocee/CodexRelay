/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 外部链接交给系统默认浏览器处理
 * @File          : 主窗口和提醒窗口共用的外链点击拦截
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
import { OpenExternalURL } from "./api.js";

// Wails WebView 对 target=_blank 的处理依赖运行时实现；统一拦截外部 HTTP(S) 链接，
// 由 Go 服务校验后调用系统默认浏览器，避免链接留在应用内或打开空白 WebView。
export function registerExternalLinkHandler(onError) {
  document.addEventListener("click", (event) => {
    if (event.defaultPrevented || event.button !== 0) return;
    const target = event.target;
    const anchor = target instanceof Element ? target.closest("a[href]") : null;
    if (!anchor || !/^https?:\/\//i.test(anchor.href || "")) return;

    event.preventDefault();
    void OpenExternalURL(anchor.href).catch((error) => {
      if (typeof onError === "function") onError(error);
    });
  });
}
