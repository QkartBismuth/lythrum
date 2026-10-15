// The music widget lists the songs in the user's folders, plays them through
// the browser's own audio stack and shows a small level meter driven by the
// WebAudio analyser node.
(function () {
  const trackEl = document.getElementById('track');
  const metaEl = document.getElementById('meta');
  const listEl = document.getElementById('list');
  const cover = document.getElementById('cover');
  const coverPlaceholder = document.getElementById('coverPlaceholder');
  const playBtn = document.getElementById('play');
  const prevBtn = document.getElementById('prev');
  const nextBtn = document.getElementById('next');
  const seek = document.getElementById('seek');
  const elapsedEl = document.getElementById('elapsed');
  const durationEl = document.getElementById('duration');
  const positionEl = document.getElementById('position');
  const canvas = document.getElementById('visual');
  const ctx2d = canvas.getContext('2d');

  let tracks = [];
  let index = 0;
  const audio = new Audio();
  audio.crossOrigin = 'anonymous';
  let analyser = null;
  let source = null;

  function fmt(seconds) {
    if (!isFinite(seconds) || seconds < 0) return '0:00';
    const m = Math.floor(seconds / 60);
    const s = Math.floor(seconds % 60);
    return m + ':' + String(s).padStart(2, '0');
  }

  function setupAnalyser() {
    if (analyser) return;
    const AudioCtx = window.AudioContext || window.webkitAudioContext;
    if (!AudioCtx) return;
    const context = new AudioCtx();
    source = context.createMediaElementSource(audio);
    analyser = context.createAnalyser();
    analyser.fftSize = 128;
    source.connect(analyser);
    analyser.connect(context.destination);
    draw();
  }

  function draw() {
    requestAnimationFrame(draw);
    const width = canvas.width, height = canvas.height;
    ctx2d.clearRect(0, 0, width, height);
    if (!analyser) return;
    const data = new Uint8Array(analyser.frequencyBinCount);
    analyser.getByteFrequencyData(data);
    const bars = 48;
    const step = Math.floor(data.length / bars) || 1;
    const barWidth = width / bars;
    for (let i = 0; i < bars; i++) {
      const value = data[i * step] / 255;
      const barHeight = Math.max(2, value * height);
      ctx2d.fillStyle = 'rgba(232,121,249,' + (0.35 + value * 0.6) + ')';
      ctx2d.fillRect(i * barWidth, height - barHeight, Math.max(1, barWidth - 2), barHeight);
    }
  }

  function renderList() {
    listEl.innerHTML = '';
    tracks.forEach((track, i) => {
      const row = document.createElement('div');
      row.textContent = track.name;
      if (i === index) row.className = 'current';
      row.addEventListener('click', () => { index = i; play(); });
      listEl.appendChild(row);
    });
  }

  function showTrack(track) {
    trackEl.textContent = track.name.replace(/\.[^.]+$/, '');
    metaEl.textContent = [track.artist, track.album].filter(Boolean).join(' — ') || 'локальный файл';
    durationEl.textContent = fmt(audio.duration);
    if (track.cover) {
      cover.src = track.cover;
      cover.style.display = 'block';
      coverPlaceholder.style.display = 'none';
    } else {
      cover.style.display = 'none';
      coverPlaceholder.style.display = 'grid';
    }
    renderList();
  }

  function play() {
    const track = tracks[index];
    if (!track) return;
    setupAnalyser();
    audio.src = track.url;
    audio.play().catch(() => { metaEl.textContent = 'не удалось включить файл'; });
    playBtn.textContent = '⏸';
    showTrack(track);
  }

  function step(delta) {
    if (!tracks.length) return;
    index = (index + delta + tracks.length) % tracks.length;
    play();
  }

  audio.addEventListener('timeupdate', () => {
    const ratio = audio.duration ? audio.currentTime / audio.duration : 0;
    seek.value = String(Math.round(ratio * 1000));
    elapsedEl.textContent = fmt(audio.currentTime);
    positionEl.textContent = fmt(audio.currentTime);
  });
  audio.addEventListener('loadedmetadata', () => { durationEl.textContent = fmt(audio.duration); });
  audio.addEventListener('ended', () => step(1));
  audio.addEventListener('play', () => { playBtn.textContent = '⏸'; });
  audio.addEventListener('pause', () => { playBtn.textContent = '▶'; });

  playBtn.addEventListener('click', () => {
    if (audio.paused) { setupAnalyser(); audio.play().catch(() => {}); } else { audio.pause(); }
  });
  prevBtn.addEventListener('click', () => step(-1));
  nextBtn.addEventListener('click', () => step(1));
  seek.addEventListener('input', () => {
    if (audio.duration) audio.currentTime = (Number(seek.value) / 1000) * audio.duration;
  });
  document.addEventListener('keydown', (event) => {
    if (event.key === 'ArrowRight') { step(1); event.preventDefault(); }
    if (event.key === 'ArrowLeft') { step(-1); event.preventDefault(); }
    if (event.key === ' ') { playBtn.click(); event.preventDefault(); }
  });

  async function load() {
    let data;
    try {
      data = await window.lythrum.call('media.list', { folder: '', limit: 300 });
    } catch (err) {
      metaEl.textContent = 'не удалось получить список треков';
      return;
    }
    tracks = (data.tracks || []).map((track) => Object.assign({}, track));
    if (!tracks.length) {
      metaEl.textContent = 'в папке Music пока нет треков';
      return;
    }
    renderList();
    showTrack(tracks[index]);
  }

  load();
})();