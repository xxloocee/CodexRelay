import { MarkDogeAnnouncementsRead } from "../../core/desktop-api.js";
import { renderAnnouncementMarkdown } from "../../announcement-markdown.js";
import { $ } from "../../core/dom.js";
import { errorMessage, setButtonLoading, setLoadingText, toast } from "../../core/feedback.js";
import { navigation, runtimeState, serverState } from "../../core/store.js";

export function createDogeAnnouncements({ loadState }) {
  function isSyncing() {
    const doge = serverState.snapshot?.doge || {};
    const notifications = doge.notifications || {};
    const waitingForFirstSync = Boolean(!notifications.lastSyncAt && !notifications.lastSyncError && !notifications.initialized);
    return Boolean(runtimeState.localAnnouncementSyncing || doge.announcementSyncing || notifications.syncing || waitingForFirstSync);
  }

  function formatDate(value) {
    const date = new Date(value || "");
    return Number.isNaN(date.getTime()) ? "时间未知" : date.toLocaleString("zh-CN", { hour12: false });
  }

  function renderAnnouncements() {
    const notifications = serverState.snapshot?.doge?.notifications || {};
    const syncing = isSyncing();
    const syncError = String(notifications.lastSyncError || "").trim();
    const badge = $("announcementBadge");
    const unread = Number(notifications.unreadCount || 0);
    badge.textContent = unread > 99 ? "99+" : String(unread);
    badge.classList.toggle("hidden", unread <= 0);
    const noticePane = $("announcementNoticePane");
    noticePane.replaceChildren();
    if (notifications.currentNotice) {
      const article = document.createElement("article");
      article.className = "announcement-current-content";
      article.append(renderAnnouncementMarkdown(notifications.currentNotice));
      noticePane.appendChild(article);
    } else {
      const empty = document.createElement("p");
      empty.className = "announcement-empty";
      if (syncing) setLoadingText(empty, true, "同步中...");
      else empty.textContent = syncError ? "公告同步失败，显示缓存" : (notifications.enabled === false ? "公告功能暂未启用" : "暂无当前公告");
      noticePane.appendChild(empty);
    }
    const timelinePane = $("announcementTimelinePane");
    timelinePane.replaceChildren();
    const announcements = notifications.announcements || [];
    if (!announcements.length) {
      const empty = document.createElement("p");
      empty.className = "announcement-empty";
      if (syncing) setLoadingText(empty, true, "同步中...");
      else empty.textContent = syncError ? "公告同步失败，显示缓存" : "暂无历史公告";
      timelinePane.appendChild(empty);
    } else {
      for (const announcement of announcements) {
        const article = document.createElement("article");
        article.className = `announcement-item type-${announcement.type || "default"}`;
        const marker = document.createElement("span");
        marker.className = "announcement-marker";
        const body = document.createElement("div");
        body.className = "announcement-item-body";
        const content = document.createElement("div");
        content.className = "announcement-item-content";
        content.append(renderAnnouncementMarkdown(announcement.content));
        const meta = document.createElement("small");
        meta.textContent = formatDate(announcement.publishDate);
        body.append(content, meta);
        article.append(marker, body);
        timelinePane.appendChild(article);
      }
    }
    setLoadingText($("announcementSyncState"), syncing, syncError ? "公告同步失败，显示缓存" : (notifications.lastSyncAt ? `更新于 ${formatDate(notifications.lastSyncAt)}` : "同步中..."));
    document.querySelectorAll("[data-announcement-tab]").forEach((button) => button.classList.toggle("active", button.dataset.announcementTab === navigation.announcementTab));
    noticePane.classList.toggle("hidden", navigation.announcementTab !== "notice");
    timelinePane.classList.toggle("hidden", navigation.announcementTab !== "timeline");
  }

  async function markRead(showError = true) {
    const ids = (serverState.snapshot?.doge?.notifications?.announcements || []).map((announcement) => announcement.id).filter((id) => Number(id) > 0);
    if (!ids.length) return;
    const button = showError ? $("markAnnouncementsRead") : null;
    setButtonLoading(button, true, "处理中...");
    try {
      await MarkDogeAnnouncementsRead(ids);
      await loadState();
    } catch (error) {
      if (showError) toast(errorMessage(error), true);
    } finally {
      setButtonLoading(button, false);
    }
  }

  function setPanel(open) {
    $("announcementPanel").classList.toggle("hidden", !open);
    $("openAnnouncements").setAttribute("aria-expanded", String(open));
    if (open) markRead(false);
  }

  function mount() {
    $("openAnnouncements").addEventListener("click", () => setPanel($("announcementPanel").classList.contains("hidden")));
    $("closeAnnouncements").addEventListener("click", () => setPanel(false));
    $("markAnnouncementsRead").addEventListener("click", () => markRead(true));
    document.querySelectorAll("[data-announcement-tab]").forEach((button) => button.addEventListener("click", () => {
      navigation.announcementTab = button.dataset.announcementTab;
      renderAnnouncements();
    }));
  }

  return { renderAnnouncements, setPanel, mount };
}
