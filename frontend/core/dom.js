export const $ = (id) => document.getElementById(id);

export function icon(name, extraClass = "") {
  const node = document.createElement("span");
  node.className = "icon icon-" + name + (extraClass ? " " + extraClass : "");
  node.setAttribute("aria-hidden", "true");
  return node;
}
