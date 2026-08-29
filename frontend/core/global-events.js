import { $ } from "./dom.js";
import { closeConfirmDialog } from "./modal.js";

export function mountGlobalEvents({ copyText }) {
  $("confirmCancel").addEventListener("click", () => closeConfirmDialog(false));
  $("confirmAccept").addEventListener("click", () => closeConfirmDialog(true));
  document.querySelectorAll("[data-copy]").forEach((button) => {
    button.addEventListener("click", () => copyText($(button.dataset.copy).textContent));
  });
}
