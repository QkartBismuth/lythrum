(function () {
  const el = (id) => document.getElementById(id);
  let coreBars = [];

  function fmtBytes(kb) {
    const units = ['КБ', 'МБ', 'ГБ', 'ТБ'];
    let value = kb, i = 0;
    while (value >= 1024 && i < units.length - 1) { value /= 1024; i++; }
    return value.toFixed(value < 10 && i > 0 ? 1 : 0) + ' ' + units[i];
  }

  function uptime(seconds) {
    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    const mins = Math.floor((seconds % 3600) / 60);
    if (days > 0) return days + ' д ' + hours + ' ч';
    if (hours > 0) return hours + ' ч ' + mins + ' мин';
    return mins + ' мин';
  }

  function rate(bytes) {
    if (bytes > 1048576) return (bytes / 1048576).toFixed(1) + ' МБ/с';
    if (bytes > 1024) return (bytes / 1024).toFixed(0) + ' КБ/с';
    return Math.round(bytes) + ' Б/с';
  }

  async function refresh() {
    let s;
    try {
      s = await window.lythrum.call('sysmon.stats', {});
    } catch (err) {
      return;
    }
    el('cpu').textContent = s.cpu.percent.toFixed(0) + '%';
    el('cpuBar').style.width = s.cpu.percent.toFixed(1) + '%';

    const cores = s.cpu.perCore || [];
    if (coreBars.length !== cores.length) {
      el('cores').innerHTML = '';
      coreBars = cores.map(() => {
        const bar = document.createElement('i');
        el('cores').appendChild(bar);
        return bar;
      });
    }
    cores.forEach((v, i) => {
      coreBars[i].style.height = Math.max(2, v) + '%';
      coreBars[i].style.opacity = String(0.35 + Math.min(v, 100) / 160);
    });

    el('mem').textContent = fmtBytes(s.memory.usedKb) + ' / ' + fmtBytes(s.memory.totalKb);
    el('memBar').style.width = s.memory.percent.toFixed(1) + '%';

    const disk = (s.disks || [])[0];
    if (disk) {
      el('diskRow').style.display = '';
      el('diskName').textContent = disk.mount === '/' ? 'Диск /' : 'Диск ' + disk.mount;
      el('disk').textContent = disk.percent.toFixed(0) + '% свободно ' + fmtBytes(disk.freeKb);
      el('diskBar').style.width = (100 - disk.percent).toFixed(1) + '%';
    } else {
      el('diskRow').style.display = 'none';
    }

    el('load').textContent = 'нагрузка ' + s.load.one.toFixed(2);
    el('net').textContent = 'сеть ↓' + rate(s.network.rxPerSecond) + ' ↑' + rate(s.network.txPerSecond);
    const sensor = (s.sensors || [])[0];
    el('temp').textContent = sensor ? sensor.label + ' ' + sensor.celsius.toFixed(0) + '°' : 'темп —';
    el('up').textContent = 'в работе ' + uptime(s.uptimeSeconds);
  }

  refresh();
  setInterval(refresh, 1000);
})();
