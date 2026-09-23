// Cache generated markup and retain unchanged event nodes during SSE refreshes.
const markupCache = new WeakMap();
function markup(id) {
  const element = $(id);
  return {
    set html(value) {
      if (markupCache.get(element) === value) return;
      markupCache.set(element, value);
      if (id !== "events") {
        element.innerHTML = value;
        return;
      }
      const template = document.createElement("template");
      template.innerHTML = value;
      const existing = new Map(
        [...element.children].map((node) => [node.dataset.index, node]),
      );
      const retained = new Set();
      let position = 0;
      for (const incoming of [...template.content.children]) {
        const current = existing.get(incoming.dataset.index);
        const node =
          current && current.innerHTML === incoming.innerHTML
            ? current
            : incoming;
        const reference = element.children[position] || null;
        if (node !== reference) element.insertBefore(node, reference);
        retained.add(node);
        position++;
      }
      for (const node of [...element.children]) {
        if (!retained.has(node)) node.remove();
      }
    },
  };
}
let renderFrame = 0;
function scheduleRender() {
  if (renderFrame) return;
  renderFrame = requestAnimationFrame(() => {
    renderFrame = 0;
    render();
  });
}
function setNavigation(open) {
  document.body.classList.toggle("nav-open", open);
  $("menu").setAttribute("aria-expanded", String(open));
  if (!open && $("sidebar").contains(document.activeElement)) $("menu").focus();
}
