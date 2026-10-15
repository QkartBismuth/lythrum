(function () {
  const hm = document.getElementById('hm');
  const ss = document.getElementById('ss');
  const date = document.getElementById('date');
  const cal = document.getElementById('cal');
  const events = document.getElementById('events');

  function pad(n) { return String(n).padStart(2, '0'); }

  async function refreshCalendar() {
    let month;
    try {
      month = await window.lythrum.call('clock.calendar', {});
    } catch (err) {
      return;
    }
    cal.innerHTML = '';
    for (const name of month.weekdayNames) {
      const cell = document.createElement('div');
      cell.className = 'dow';
      cell.textContent = name;
      cal.appendChild(cell);
    }
    const marked = new Set();
    for (const e of month.events || []) {
      marked.add(String(e.start).slice(0, 10));
    }
    for (const week of month.weeks) {
      for (const iso of week) {
        const cell = document.createElement('div');
        const day = Number(iso.slice(8, 10));
        cell.className = 'day';
        if (iso.slice(0, 7) !== month.today.slice(0, 7)) cell.classList.add('other');
        if (iso === month.today) cell.classList.add('today');
        if (marked.has(iso)) cell.classList.add('event');
        cell.textContent = String(day);
        cal.appendChild(cell);
      }
    }

    try {
      const list = await window.lythrum.call('clock.events', { days: 14 });
      events.innerHTML = '';
      for (const e of (list.events || []).slice(0, 4)) {
        const item = document.createElement('div');
        item.className = 'item';
        const when = document.createElement('span');
        when.className = 'when';
        const t = String(e.start).replace(/[-:T]/g, '').slice(0, 6);
        when.textContent = e.allDay ? 'весь день' : t.slice(0, 2) + ':' + t.slice(2, 4);
        const title = document.createElement('span');
        title.textContent = e.title;
        item.appendChild(when);
        item.appendChild(title);
        events.appendChild(item);
      }
    } catch (err) { /* no calendar files */ }
  }

  async function tick() {
    try {
      const now = await window.lythrum.call('clock.now', {});
      hm.textContent = now.time;
      ss.textContent = ':' + now.seconds;
      date.textContent = now.weekday + ', ' + now.date;
    } catch (err) {
      const d = new Date();
      hm.textContent = pad(d.getHours()) + ':' + pad(d.getMinutes());
      ss.textContent = ':' + pad(d.getSeconds());
      date.textContent = d.toLocaleDateString('ru-RU');
    }
  }

  tick();
  setInterval(tick, 1000);
  refreshCalendar();
  setInterval(refreshCalendar, 5 * 60 * 1000);
})();
