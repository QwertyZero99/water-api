/**
 * Make a maplibre popup draggable by its header,
 * The map itself will not move while dragging.
 */
export function makePopupDraggable(map, popup, headerSelector) {
  const popupElement = popup.getElement();
  const header = popupElement.querySelector(headerSelector);

  if (!header) return;

  header.classList.add("aquany-popup-drag-handle");

  let dragging = false;
  let lastX = 0;
  let lastY = 0;
  let restoreMapDrag = false;

  function onPointerDown(event) {
    if (event.button !== 0) return;

    // Don't interfere with buttons or links.
    if (event.target.closest("button, a, input")) return;

    dragging = true;
    lastX = event.clientX;
    lastY = event.clientY;

    restoreMapDrag = map.dragPan.isEnabled();
    if (restoreMapDrag) map.dragPan.disable();

    header.setPointerCapture(event.pointerId);
    popupElement.classList.add("aquany-popup-dragging");

    event.preventDefault();
    event.stopPropagation();
  }

  function onPointerMove(event) {
    if (!dragging) return;

    const dx = event.clientX - lastX;
    const dy = event.clientY - lastY;

    lastX = event.clientX;
    lastY = event.clientY;

    const current = map.project(popup.getLngLat());

    const next = map.unproject([
      current.x + dx,
      current.y + dy
    ]);

    popup.setLngLat(next);

    event.stopPropagation();
  }

  function stopDragging(event) {
    if (!dragging) return;

    dragging = false;

    if (header.hasPointerCapture(event.pointerId)) {
      header.releasePointerCapture(event.pointerId);
    }

    if (restoreMapDrag) map.dragPan.enable();

    popupElement.classList.remove("aquany-popup-dragging");
  }

  header.addEventListener("pointerdown", onPointerDown);
  header.addEventListener("pointermove", onPointerMove);
  header.addEventListener("pointerup", stopDragging);
  header.addEventListener("pointercancel", stopDragging);

  popup.on("close", () => {
    if (dragging && restoreMapDrag) map.dragPan.enable();
    dragging = false;
  });
}