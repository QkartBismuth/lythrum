// Bridge between a widget page and the Go backend. The page posts messages to
// the shell through the script message handler and gets answers back as
// promises, which keeps widget code free of network calls.
(function () {
  // The shell renders every page off screen, where CSS animations never tick.
  document.documentElement.classList.add('offscreen');
  let nextId = 0;
  const pending = new Map();

  window.__lythrumResolve = function (id, payload) {
    const entry = pending.get(id);
    if (!entry) return;
    pending.delete(id);
    if (payload && payload.error) entry.reject(new Error(payload.error));
    else entry.resolve(payload);
  };

  // An off-screen window never gets real focus, so document.activeElement is
  // the body and a typed key would go nowhere. The element the pointer used
  // last is remembered instead and edited directly: a synthetic key event
  // cannot change the value of an input on its own.
  let focused = null;

  function post(method, params) {
    if (!window.webkit || !window.webkit.messageHandlers || !window.webkit.messageHandlers.lythrum) {
      return Promise.reject(new Error('нет связи с оболочкой'));
    }
    const id = String(++nextId);
    return new Promise((resolve, reject) => {
      pending.set(id, { resolve, reject });
      window.webkit.messageHandlers.lythrum.postMessage(
        JSON.stringify({ id, method, params: params || {} }));
      setTimeout(() => {
        if (pending.delete(id)) reject(new Error('оболочка не ответила: ' + method));
      }, 30000);
    });
  }

  window.lythrum = { call: post };

  // The shell paints tiles off screen and forwards the input here, so a page
  // cannot tell the difference between a real pointer and a forwarded one.
  window.__lythrumPointer = function (x, y, kind) {
    let target = document.elementFromPoint(x, y);
    if (!target) return;
    // A browser turns a click on a label into a click on the field it names,
    // and the off-screen input is what the page actually wants to hear about.
    if (kind === 'click' && target.tagName === 'LABEL' && target.control) {
      target = target.control;
    }
    if (kind === 'click') {
      focused = target;
      placeCaret(target, x, y);
    }
    target.dispatchEvent(new PointerEvent(kind === 'click' ? 'pointerdown' : 'pointermove', {
      bubbles: true, cancelable: true, clientX: x, clientY: y, pointerId: 1, isPrimary: true,
    }));
    if (kind === 'click') {
      target.dispatchEvent(new PointerEvent('pointerup', {
        bubbles: true, cancelable: true, clientX: x, clientY: y, pointerId: 1, isPrimary: true,
      }));
      target.click();
    }
  };

  function isEditable(node) {
    if (!node) return false;
    if (node.isContentEditable) return true;
    if (node.tagName === 'TEXTAREA') return true;
    return node.tagName === 'INPUT' &&
      ['text', 'search', 'url', 'tel', 'password', 'number', 'email', ''].includes(node.type);
  }

  function editValue(node, op, text) {
    const start = node.selectionStart === null ? node.value.length : node.selectionStart;
    const end = node.selectionEnd === null ? node.value.length : node.selectionEnd;
    let from = start;
    let to = end;
    let insert = '';
    if (op === 'backspace') {
      if (from === to) from = Math.max(0, from - 1);
    } else if (op === 'delete') {
      if (from === to) to = Math.min(node.value.length, to + 1);
    } else if (op === 'insert') {
      insert = text;
    } else {
      return;
    }
    node.value = node.value.slice(0, from) + insert + node.value.slice(to);
    const caret = from + insert.length;
    if (node.setSelectionRange) {
      try { node.setSelectionRange(caret, caret); } catch (err) { /* number inputs refuse */ }
    }
    node.dispatchEvent(new Event('input', { bubbles: true }));
    node.dispatchEvent(new Event('change', { bubbles: true }));
  }

  // A synthetic click does not put a caret into a field, so the typed text
  // would always land at the end. The page can still say where the character
  // would have gone.
  function placeCaret(node, x, y) {
    if (!isEditable(node) || !node.setSelectionRange) return;
    let offset = null;
    if (document.caretPositionFromPoint) {
      const pos = document.caretPositionFromPoint(x, y);
      if (pos) offset = pos.offset;
    } else if (document.caretRangeFromPoint) {
      const range = document.caretRangeFromPoint(x, y);
      if (range) offset = range.startOffset;
    }
    const at = offset === null || offset < 0 ? node.value.length : offset;
    try { node.setSelectionRange(at, at); } catch (err) { /* not supported */ }
  }

  window.__lythrumKey = function (name, text) {
    const key = name === 'space' ? ' ' : (text || name);
    const target = (document.activeElement && document.activeElement !== document.body)
      ? document.activeElement
      : (focused || document.activeElement || document.body);
    const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true });
    target.dispatchEvent(event);
    if (event.defaultPrevented) return;

    if (name === 'Return' || name === 'KP_Enter') {
      if (target.tagName === 'BUTTON') { target.click(); return; }
      if (isEditable(target)) { editValue(target, 'insert', '\n'); }
      return;
    }
    if (name === 'space' || name === ' ' || key === ' ') {
      if (target.tagName === 'BUTTON') { target.click(); return; }
    }
    if (name === 'BackSpace' || name === 'Delete') {
      if (isEditable(target)) {
        editValue(target, name === 'BackSpace' ? 'backspace' : 'delete', '');
      }
      return;
    }
    if (name === 'Left' || name === 'Right' || name === 'Home' || name === 'End') {
      if (!isEditable(target) || !target.setSelectionRange) return;
      const at = target.selectionStart === null ? target.value.length : target.selectionStart;
      let caret = at;
      if (name === 'Left') caret = Math.max(0, at - 1);
      if (name === 'Right') caret = Math.min(target.value.length, at + 1);
      if (name === 'Home') caret = 0;
      if (name === 'End') caret = target.value.length;
      try { target.setSelectionRange(caret, caret); } catch (err) { /* not supported */ }
      return;
    }
    if (key.length === 1 && isEditable(target)) editValue(target, 'insert', key);
  };

  // Theme colours come from the shell so widgets match the desktop.
  post('shell.theme').then((theme) => {
    window.lythrumTheme = theme || {};
    if (theme && theme.background) {
      document.documentElement.style.setProperty('--shell-bg', theme.background);
    }
  }).catch(() => { window.lythrumTheme = {}; });
})();