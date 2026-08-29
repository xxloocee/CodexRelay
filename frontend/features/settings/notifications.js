import { SetTaskNotification, TestTaskNotification } from "../../core/desktop-api.js";
import { $ } from "../../core/dom.js";
import { errorMessage, setButtonLoading, toast } from "../../core/feedback.js";
import { drafts, serverState } from "../../core/store.js";

const eventInputs = {
  taskCompleted: "taskNotificationTaskCompleted",
  taskAborted: "taskNotificationTaskAborted",
  tokenRequestFailed: "taskNotificationTokenRequestFailed",
  tokenAutoSwitched: "taskNotificationTokenAutoSwitched",
  tokenAutoSwitchFailed: "taskNotificationTokenAutoSwitchFailed",
  accountBalanceLow: "taskNotificationAccountBalanceLow",
  subscriptionBalanceLow: "taskNotificationSubscriptionBalanceLow",
};

export function createNotifications({ loadState }) {
  function renderTaskNotification() {
    const notification = serverState.snapshot?.taskNotification || {};
    if (!drafts.taskNotificationDirty) {
      const events = notification.events || {
        taskCompleted: true,
        taskAborted: true,
        tokenRequestFailed: true,
        tokenAutoSwitched: true,
        tokenAutoSwitchFailed: true,
        accountBalanceLow: true,
        subscriptionBalanceLow: true,
      };
      $("taskNotificationEnabled").checked = Boolean(notification.enabled);
      $("taskNotificationWebhookUrl").value = notification.webhookUrl || "";
      for (const [key, id] of Object.entries(eventInputs)) $(id).checked = Boolean(events[key]);
      $("taskNotificationIdleGraceSeconds").value = String(notification.idleGraceSeconds || 5);
      $("taskNotificationRequestTimeoutSeconds").value = String(notification.requestTimeoutSeconds || 10);
      $("taskNotificationMaxAttempts").value = String(notification.maxAttempts || 0);
    }
    const status = notification.status || {};
    $("taskNotificationQueueState").textContent = `候选 ${status.pending || 0} · 待投递 ${status.outbox || 0} · 失败 ${status.dead || 0}`;
    $("taskNotificationError").textContent = status.lastError || "";
  }

  function markTaskNotificationDirty() {
    drafts.taskNotificationDirty = true;
    drafts.taskNotificationDraftRevision += 1;
  }

  function notificationPayload() {
    const events = {};
    for (const [key, id] of Object.entries(eventInputs)) events[key] = $(id).checked;
    return {
      enabled: $("taskNotificationEnabled").checked,
      webhookUrl: $("taskNotificationWebhookUrl").value.trim(),
      events,
      idleGraceSeconds: Number($("taskNotificationIdleGraceSeconds").value),
      requestTimeoutSeconds: Number($("taskNotificationRequestTimeoutSeconds").value),
      maxAttempts: Number($("taskNotificationMaxAttempts").value),
    };
  }

  async function saveTaskNotification(button = $("saveTaskNotification"), successMessage = "任务完成通知设置已保存") {
    const draftRevision = drafts.taskNotificationDraftRevision;
    setButtonLoading(button, true, "保存中...");
    try {
      await SetTaskNotification(notificationPayload());
      if (draftRevision === drafts.taskNotificationDraftRevision) drafts.taskNotificationDirty = false;
      await loadState();
      toast(successMessage);
      return true;
    } catch (error) {
      await loadState();
      toast(errorMessage(error), true);
      return false;
    } finally {
      setButtonLoading(button, false);
    }
  }

  async function testTaskNotification() {
    const button = $("testTaskNotification");
    if (!(await saveTaskNotification(button, "设置已保存，正在测试通知..."))) return;
    setButtonLoading(button, true, "测试中...");
    try {
      await TestTaskNotification();
      toast("测试通知已发送");
    } catch (error) {
      toast(errorMessage(error), true);
    } finally {
      setButtonLoading(button, false);
    }
  }

  function mount() {
    $("saveTaskNotification").addEventListener("click", () => saveTaskNotification());
    $("testTaskNotification").addEventListener("click", testTaskNotification);
    const inputIds = [
      "taskNotificationEnabled",
      "taskNotificationWebhookUrl",
      "taskNotificationIdleGraceSeconds",
      "taskNotificationRequestTimeoutSeconds",
      "taskNotificationMaxAttempts",
      ...Object.values(eventInputs),
    ];
    for (const id of inputIds) $(id).addEventListener("input", markTaskNotificationDirty);
    $("taskNotificationEnabled").addEventListener("change", markTaskNotificationDirty);
    for (const id of Object.values(eventInputs)) $(id).addEventListener("change", markTaskNotificationDirty);
  }

  return { renderTaskNotification, mount };
}
