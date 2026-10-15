// The photos widget walks the picture folders of the user, shows them as a
// slideshow and plays short videos. Files come from the shell over the
// lythrum scheme, so nothing is opened outside the home directory.
(function () {
  const stage = document.getElementById('stage');
  const empty = document.getElementById('empty');
  const caption = document.getElementById('caption');
  const count = document.getElementById('count');
  const playBtn = document.getElementById('play');
  const prevBtn = document.getElementById('prev');
  const nextBtn = document.getElementById('next');

  let items = [];
  let index = 0;
  let timer = null;
  let playing = true;
  let current = null;
  const INTERVAL = 4200;

  function clearStage() {
    if (!current) return;
    if (current.tagName === 'VIDEO') current.pause();
    current.remove();
    current = null;
  }

  function show(item) {
    if (!item) return;
    clearStage();
    const node = document.createElement(item.kind === 'video' ? 'video' : 'img');
    if (node.tagName === 'VIDEO') {
      node.muted = true;
      node.loop = true;
      node.playsInline = true;
      node.src = item.url;
      node.addEventListener('error', () => schedule(1500));
      node.play().catch(() => {});
    } else {
      node.src = item.url;
      node.addEventListener('error', () => schedule(1500));
    }
    node.addEventListener('load', () => node.classList.add('show'));
    node.addEventListener('loadeddata', () => node.classList.add('show'));
    stage.appendChild(node);
    // The new picture fades in over the old one, which is already gone, so the
    // stage simply shows the new frame.
    requestAnimationFrame(() => node.classList.add('show'));
    current = node;
    caption.textContent = item.name;
    count.textContent = (index + 1) + ' / ' + items.length;
    if (item.kind === 'video') {
      const duration = item.kind === 'video' && item.durationMs ? item.durationMs : 6000;
      schedule(duration);
    } else {
      schedule(INTERVAL);
    }
  }

  function schedule(ms) {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => step(1), ms);
  }

  function step(delta) {
    if (!items.length) return;
    index = (index + delta + items.length) % items.length;
    show(items[index]);
  }

  function setPlaying(on) {
    playing = on;
    playBtn.textContent = on ? '⏸' : '▶';
    playBtn.setAttribute('aria-pressed', on ? 'true' : 'false');
    if (on) {
      schedule(INTERVAL);
    } else if (timer) {
      clearTimeout(timer);
      timer = null;
    }
    if (current && current.tagName === 'VIDEO') {
      if (on) current.play().catch(() => {});
      else current.pause();
    }
  }

  playBtn.addEventListener('click', () => setPlaying(!playing));
  prevBtn.addEventListener('click', () => { if (playing) step(-1); else step(-1); });
  nextBtn.addEventListener('click', () => step(1));
  stage.addEventListener('click', () => step(1));

  document.addEventListener('keydown', (event) => {
    if (event.key === 'ArrowRight' || event.key === ' ') { step(1); event.preventDefault(); }
    if (event.key === 'ArrowLeft') { step(-1); event.preventDefault(); }
  });

  async function load() {
    let data;
    try {
      data = await window.lythrum.call('media.list', { limit: 300 });
    } catch (err) {
      empty.textContent = 'Не удалось получить список файлов';
      return;
    }
    items = (data.photos || []).concat(data.videos || []);
    if (!items.length) {
      empty.textContent = 'В папках Pictures, Videos и Downloads пока нет медиафайлов';
      return;
    }
    empty.remove();
    index = 0;
    show(items[0]);
  }

  load();
  setInterval(load, 60000);
})();