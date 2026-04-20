const MONTH_NAMES = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const DAY_NAMES = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

export function formatTime(ts: string): string {
  const d = new Date(ts);
  const now = new Date();

  const hours24 = d.getHours();
  const ampm = hours24 >= 12 ? 'PM' : 'AM';
  let hours = hours24 % 12;
  if (hours === 0) hours = 12;
  const m = String(d.getMinutes()).padStart(2, '0');
  const s = String(d.getSeconds()).padStart(2, '0');
  const ms = String(d.getMilliseconds()).padStart(3, '0');
  const timePart = `${hours}:${m}:${s}.${ms} ${ampm}`;

  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const recordDay = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  const diffDays = Math.round((today.getTime() - recordDay.getTime()) / 86400000);

  let datePart: string;
  if (diffDays === 0) {
    datePart = 'Today';
  } else if (diffDays === 1) {
    datePart = 'Yesterday';
  } else if (diffDays >= 0 && diffDays <= 6) {
    datePart = DAY_NAMES[d.getDay()];
  } else {
    datePart = MONTH_NAMES[d.getMonth()] + ' ' + d.getDate();
  }

  if (d.getFullYear() !== now.getFullYear()) {
    datePart = d.getFullYear() + ' ' + datePart;
  }

  return datePart + ' ' + timePart;
}

export function formatDuration(ms: number): string {
  if (ms < 1) return ms.toFixed(2) + 'ms';
  if (ms < 1000) return ms.toFixed(1) + 'ms';
  return (ms / 1000).toFixed(2) + 's';
}

export function durationColor(ms: number): string {
  if (ms < 100) return 'text-green-400';
  if (ms < 500) return 'text-yellow-400';
  if (ms < 2000) return 'text-amber-400';
  return 'text-red-400';
}

export function formatQueryParams(raw: string): { text: string; parts: { key: string; value: string }[] } {
  if (!raw) return { text: '', parts: [] };
  const params = new URLSearchParams(raw);
  const parts: { key: string; value: string }[] = [];
  for (const [key, value] of params) {
    parts.push({ key: decodeURIComponent(key), value: decodeURIComponent(value) });
  }
  const text = parts.map((p) => `${p.key}=${p.value}`).join(', ');
  return { text, parts };
}
