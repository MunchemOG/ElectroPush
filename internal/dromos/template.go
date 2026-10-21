package dromos

// The power page's stylesheet, down to the variables, so the pages epsh opens
// look like one tool. Everything is inline: this opens on a laptop that may be
// on the robot's Wi-Fi with no route anywhere.
const runPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} - epsh run</title>
<style>
  :root {
    --bg: #ffffff; --fg: #1b1f24; --muted: #6b7684; --line: #e3e6ea;
    --panel: #f7f8fa; --accent: #4C9AFF; --good: #36B37E; --fair: #FFAB00; --bad: #FF5630;
  }
  @media (prefers-color-scheme: dark) {
    :root { --bg: #14171a; --fg: #e6e9ec; --muted: #98a2ad; --line: #2a2f36;
            --panel: #1b1f24; --accent: #6BB0FF; }
  }
  * { box-sizing: border-box; }
  body { margin: 0; padding: 24px; background: var(--bg); color: var(--fg);
         font: 14px/1.5 ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif; }
  .wrap { max-width: 1280px; margin: 0 auto; }
  h1 { font-size: 20px; margin: 0 0 2px; }
  h2 { font-size: 13px; text-transform: uppercase; letter-spacing: .04em;
       color: var(--muted); margin: 24px 0 8px; }
  .sub { color: var(--muted); margin-bottom: 20px; }
  .cards { display: flex; flex-wrap: wrap; gap: 12px; margin-bottom: 20px; }
  .card { background: var(--panel); border: 1px solid var(--line); border-radius: 10px;
          padding: 12px 16px; min-width: 150px; flex: 1; }
  .card .k { color: var(--muted); font-size: 12px; text-transform: uppercase; letter-spacing: .04em; }
  .card .v { font-size: 22px; font-weight: 600; margin-top: 2px; }
  .card .v small { font-size: 13px; font-weight: 400; color: var(--muted); }
  .grid { display: grid; grid-template-columns: minmax(280px, 420px) 1fr; gap: 16px; align-items: start; }
  @media (max-width: 860px) { .grid { grid-template-columns: 1fr; } }
  svg { width: 100%; height: auto; background: var(--panel);
        border: 1px solid var(--line); border-radius: 10px; display: block; }
  .lane { margin-bottom: 10px; }
  .lane .name { font-size: 12px; color: var(--muted); margin: 0 0 3px 2px; }
  .lane svg { cursor: crosshair; }
  .legend { display: flex; flex-wrap: wrap; gap: 12px; margin: 4px 0 0 2px; color: var(--muted); font-size: 12px; }
  .legend .key { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; user-select: none; }
  .legend .key.off { opacity: .35; text-decoration: line-through; }
  .swatch { width: 11px; height: 11px; border-radius: 3px; display: inline-block; }
  .controls { display: flex; gap: 10px; align-items: center; margin: 6px 0 4px; }
  .controls input[type=range] { flex: 1; }
  button { background: var(--panel); color: var(--fg); border: 1px solid var(--line);
           border-radius: 8px; padding: 4px 12px; font: inherit; cursor: pointer; }
  .readout { font-size: 12px; color: var(--muted); min-height: 20px; font-variant-numeric: tabular-nums; }
  .readout b { color: var(--fg); font-weight: 600; }
  table { border-collapse: collapse; width: 100%; font-size: 13px; }
  th, td { text-align: left; padding: 7px 9px; border-bottom: 1px solid var(--line); white-space: nowrap; }
  th { color: var(--muted); font-weight: 600; font-size: 11px; text-transform: uppercase; letter-spacing: .04em; }
  td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
  table { width: auto; min-width: 60%; }
  .note { background: var(--panel); border: 1px solid var(--line); border-left: 3px solid var(--fair);
          border-radius: 8px; padding: 10px 14px; margin-bottom: 16px; }
  .hint { color: var(--muted); font-size: 12px; margin-top: 6px; }
  footer { margin-top: 28px; color: var(--muted); font-size: 12px; }
  .axis { fill: var(--muted); font-size: 10px; }
  .gridline { stroke: var(--line); stroke-width: 1; }
  .cursor { stroke: var(--fg); stroke-width: 1; opacity: .55; }
  .band { fill: var(--accent); fill-opacity: .16; }
</style>
</head>
<body>
<div class="wrap">
  <h1>{{.Title}}</h1>
  <div class="sub" id="sub"></div>
  <div id="notes"></div>
  <div class="cards" id="cards"></div>

  <div class="grid">
    <div>
      <h2>On the field</h2>
      <svg id="field" viewBox="-4 -4 152 152"></svg>
      <div class="legend">
        <span class="key"><i class="swatch" style="background:var(--muted)"></i>planned</span>
        <span class="key"><i class="swatch" style="background:var(--good)"></i>under 0.5 in off</span>
        <span class="key"><i class="swatch" style="background:var(--fair)"></i>under 1.5 in</span>
        <span class="key"><i class="swatch" style="background:var(--bad)"></i>more</span>
      </div>
    </div>
    <div>
      <h2>Over time</h2>
      <div class="controls">
        <button id="play">Play</button>
        <input type="range" id="scrub" min="0" max="1000" value="0">
      </div>
      <div class="readout" id="readout"></div>
      <div id="lanes"></div>
      <div class="hint">Drag across a lane to zoom into that stretch, double-click to zoom out. Click a name to hide its line.</div>
    </div>
  </div>

  <h2>Where each path ended</h2>
  <table id="ends"></table>
  <div id="arrivalsWrap"></div>

  <footer id="foot"></footer>
</div>

<script>
const D = {{.Data}};
const S = D.s;
const N = S.t.length;
const svgNS = "http://www.w3.org/2000/svg";
const css = name => getComputedStyle(document.documentElement).getPropertyValue(name).trim();
const palette = ["#4C9AFF","#FF5630","#36B37E","#FFAB00","#6554C0","#00B8D9","#FF8B00","#8777D9","#57D9A3","#FF7452"];

function el(tag, attrs, parent) {
  const e = document.createElementNS(svgNS, tag);
  for (const k in attrs) e.setAttribute(k, attrs[k]);
  if (parent) parent.appendChild(e);
  return e;
}
const fmt = (x, d) => (x === null || x === undefined || isNaN(x)) ? "–" : x.toFixed(d);
const deg = r => r * 180 / Math.PI;

// Index of the last sample at or before t.
function at(times, t) {
  let lo = 0, hi = times.length - 1;
  if (hi < 0) return -1;
  if (t <= (times[0] ?? 0)) return 0;
  while (hi - lo > 1) {
    const mid = (lo + hi) >> 1;
    if ((times[mid] ?? -Infinity) <= t) lo = mid; else hi = mid;
  }
  return (times[hi] ?? Infinity) <= t ? hi : lo;
}

// ---- Summary ----------------------------------------------------------------
const errs = S.err.filter(e => e !== null);
const worstErr = errs.length ? Math.max(...errs) : null;
const meanErr = errs.length ? errs.reduce((a, b) => a + b, 0) / errs.length : null;
const volts = (S.v || []).filter(v => v !== null && v > 0);
const ends = D.ends || [];
const worstEnd = ends.reduce((w, e) => Math.max(w, Math.hypot(e.dx, e.dy)), 0);

document.getElementById("sub").textContent =
  D.date + " · " + D.seconds.toFixed(1) + " s" +
  (D.power ? " · power from " + D.power.opmode : "") +
  (D.loop ? " · loop profile from " + D.loop.opmode : "");

const notes = document.getElementById("notes");
if (D.full) notes.insertAdjacentHTML("beforeend", '<div class="note">The recording filled up, so the end of the run is missing.</div>');
if (D.aligned === "clock") notes.insertAdjacentHTML("beforeend",
  '<div class="note">Lined up by the robot\'s clock, which the run stamps to the second: the power and loop lanes may sit up to a second off. The battery barely moved during this run, so there was no shape in it to line the traces up by.</div>');

const cards = [
  ["Worst end", ends.length ? fmt(worstEnd, 2) : "–", "in"],
  ["Off the path", fmt(meanErr, 2), "in on average"],
  ["Furthest off", fmt(worstErr, 2), "in"],
];
if (volts.length) cards.push(["Battery", fmt(Math.min(...volts), 2), "V at its lowest"]);
if (D.power) cards.push(["Motors", fmt(Math.max(...D.power.total.filter(x => x !== null)), 1), "A at peak, together"]);
if (D.loop) {
  const total = D.loop.slices.reduce((a, s) => a + (s.b - s.a), 0);
  const team = D.loop.slices.filter(s => s.team).reduce((a, s) => a + (s.b - s.a), 0);
  cards.push(["Loop", total ? Math.round(100 * team / total) : "–", "% in your code"]);
}
document.getElementById("cards").innerHTML = cards.map(c =>
  '<div class="card"><div class="k">' + c[0] + '</div><div class="v">' + c[1] + '<small> ' + c[2] + '</small></div></div>').join("");

// ---- Field ------------------------------------------------------------------
const field = document.getElementById("field");
const F = D.field;
const world = el("g", {transform: "translate(0," + F + ") scale(1,-1)"}, field);
el("rect", {x: 0, y: 0, width: F, height: F, fill: "none", stroke: css("--line"), "stroke-width": 0.6}, world);
for (let g = 24; g < F; g += 24) {
  el("line", {x1: g, y1: 0, x2: g, y2: F, class: "gridline", "stroke-width": 0.25}, world);
  el("line", {x1: 0, y1: g, x2: F, y2: g, class: "gridline", "stroke-width": 0.25}, world);
}
for (const p of D.paths || []) {
  for (const seg of p.segments) {
    const pts = seg.filter(q => q[0] !== null && q[1] !== null).map(q => q[0] + "," + q[1]).join(" ");
    el("polyline", {points: pts, fill: "none", stroke: css("--muted"), "stroke-width": 0.8, "stroke-dasharray": "2 1.2"}, world);
  }
}
const bucket = e => e === null ? null : e < 0.5 ? 0 : e < 1.5 ? 1 : 2;
const colours = [css("--good"), css("--fair"), css("--bad")];
const traces = [[], [], []];
for (let i = 1; i < N; i++) {
  const b = bucket(S.err[i]);
  if (b === null || S.x[i] === null || S.x[i-1] === null) continue;
  traces[b].push("M" + S.x[i-1] + " " + S.y[i-1] + "L" + S.x[i] + " " + S.y[i]);
}
traces.forEach((d, b) => { if (d.length) el("path", {d: d.join(""), stroke: colours[b], "stroke-width": 1.1, fill: "none", "stroke-linecap": "round"}, world); });

for (const pose of D.poses || []) {
  const [dx, dy] = pose.designed, [ux, uy] = pose.used;
  if (dx !== null) el("circle", {cx: dx, cy: dy, r: 1.6, fill: "none", stroke: css("--fg"), "stroke-width": 0.4}, world);
  if (ux !== null && (ux !== dx || uy !== dy)) el("circle", {cx: ux, cy: uy, r: 0.9, fill: css("--accent")}, world);
  if (dx !== null) {
    const label = el("text", {x: dx + 2.2, y: -(dy) + 1, transform: "scale(1,-1)", fill: css("--muted"), style: "font-size:3.4px"}, world);
    label.textContent = pose.name;
  }
}

const robot = el("g", {}, world);
el("rect", {x: -9, y: -9, width: 18, height: 18, rx: 1.5, fill: css("--accent"), "fill-opacity": 0.18, stroke: css("--accent"), "stroke-width": 0.6}, robot);
el("path", {d: "M3 -3 L9 0 L3 3 Z", fill: css("--accent")}, robot);
const target = el("circle", {r: 0.9, fill: css("--fg")}, world);

// ---- Lanes ------------------------------------------------------------------
const W = 900, ML = 46, MR = 12, MT = 8, MB = 20, PW = W - ML - MR;
let view = [0, D.seconds || 1];
let now = 0;
const lanes = [];

function lane(title, height, unit, series, kind) {
  const wrap = document.createElement("div");
  wrap.className = "lane";
  wrap.innerHTML = '<div class="name">' + title + '</div>';
  const svg = el("svg", {viewBox: "0 0 " + W + " " + (height + MT + MB)});
  wrap.appendChild(svg);
  if (series && series.length > 1 && kind !== "blocks") {
    const legend = document.createElement("div");
    legend.className = "legend";
    series.forEach(s => {
      const key = document.createElement("span");
      key.className = "key";
      key.innerHTML = '<i class="swatch" style="background:' + s.colour + '"></i>' + s.name;
      key.onclick = () => { s.off = !s.off; key.classList.toggle("off", s.off); draw(); };
      legend.appendChild(key);
    });
    wrap.appendChild(legend);
  }
  document.getElementById("lanes").appendChild(wrap);
  const L = {svg, height, unit, series, kind};
  lanes.push(L);
  attach(L);
  return L;
}

const xOf = t => ML + (t - view[0]) / (view[1] - view[0]) * PW;
const tOf = x => view[0] + (x - ML) / PW * (view[1] - view[0]);

function draw() {
  for (const L of lanes) {
    const svg = L.svg;
    while (svg.firstChild) svg.removeChild(svg.firstChild);
    const H = L.height;

    if (L.kind === "blocks") {
      for (const s of L.series) {
        if (s.b < view[0] || s.a > view[1]) continue;
        const x0 = Math.max(ML, xOf(s.a)), x1 = Math.min(ML + PW, xOf(s.b));
        const r = el("rect", {x: x0, y: MT, width: Math.max(0.5, x1 - x0), height: H,
          fill: s.team ? css("--accent") : css("--line")}, svg);
        el("title", {}, r).textContent = s.n;
      }
    } else {
      let lo = Infinity, hi = -Infinity;
      for (const s of L.series) {
        if (s.off) continue;
        for (let i = 0; i < s.t.length; i++) {
          const t = s.t[i], y = s.y[i];
          if (t == null || y == null || t < view[0] || t > view[1]) continue;
          lo = Math.min(lo, y); hi = Math.max(hi, y);
        }
      }
      if (!isFinite(lo)) { lo = 0; hi = 1; }
      if (L.zero) lo = Math.min(lo, 0);
      if (hi - lo < 1e-6) { hi += 0.5; lo -= 0.5; }
      const pad = (hi - lo) * 0.08; lo -= L.zero && lo === 0 ? 0 : pad; hi += pad;
      const yOf = y => MT + H - (y - lo) / (hi - lo) * H;

      for (let k = 0; k <= 3; k++) {
        const v = lo + (hi - lo) * k / 3, y = yOf(v);
        el("line", {x1: ML, y1: y, x2: ML + PW, y2: y, class: "gridline"}, svg);
        el("text", {x: ML - 6, y: y + 3, "text-anchor": "end", class: "axis"}, svg).textContent = v.toFixed(Math.abs(hi - lo) < 3 ? 2 : 1);
      }
      for (const s of L.series) {
        if (s.off) continue;
        const i0 = Math.max(0, at(s.t, view[0]) - 1), i1 = Math.min(s.t.length - 1, at(s.t, view[1]) + 1);
        const step = Math.max(1, Math.floor((i1 - i0) / 1500));
        let d = "", pen = false;
        for (let i = i0; i <= i1; i += step) {
          const t = s.t[i], y = s.y[i];
          if (t == null || y == null) { pen = false; continue; }
          d += (pen ? "L" : "M") + xOf(t).toFixed(1) + " " + yOf(y).toFixed(1);
          pen = true;
        }
        if (d) el("path", {d, fill: "none", stroke: s.colour, "stroke-width": 1.4}, svg);
      }
    }

    const span = view[1] - view[0];
    const tick = [0.1, 0.25, 0.5, 1, 2, 5, 10, 15, 30].find(s => span / s <= 10) || 60;
    for (let t = Math.ceil(view[0] / tick) * tick; t <= view[1]; t += tick) {
      el("text", {x: xOf(t), y: MT + H + 14, "text-anchor": "middle", class: "axis"}, svg).textContent = (+t.toFixed(2)) + "s";
    }
    L.band = el("rect", {y: MT, height: H, width: 0, class: "band", visibility: "hidden"}, svg);
    L.cursor = el("line", {y1: MT, y2: MT + H, class: "cursor"}, svg);
  }
  setTime(now);
}

function attach(L) {
  let start = null;
  const toX = ev => {
    const r = L.svg.getBoundingClientRect();
    return (ev.clientX - r.left) / r.width * W;
  };
  L.svg.addEventListener("mousedown", ev => { start = toX(ev); });
  L.svg.addEventListener("mousemove", ev => {
    const x = toX(ev);
    setTime(Math.min(view[1], Math.max(view[0], tOf(x))));
    if (start !== null) {
      L.band.setAttribute("visibility", "visible");
      L.band.setAttribute("x", Math.min(start, x));
      L.band.setAttribute("width", Math.abs(x - start));
    }
  });
  window.addEventListener("mouseup", ev => {
    if (start === null) return;
    const x = toX(ev), a = tOf(Math.min(start, x)), b = tOf(Math.max(start, x));
    start = null;
    if (Math.abs(b - a) > (view[1] - view[0]) / 100) { view = [Math.max(0, a), Math.min(D.seconds, b)]; draw(); }
    else L.band && L.band.setAttribute("visibility", "hidden");
  });
  L.svg.addEventListener("dblclick", () => { view = [0, D.seconds || 1]; draw(); });
}

const T = S.t;
lane("Off the path (in)", 70, "in", [{name: "off the path", colour: css("--bad"), t: T, y: S.err}]).zero = true;

const battery = [];
if (S.v && S.v.some(v => v !== null && v > 0)) battery.push({name: "battery, as ElectroDromos read it", colour: palette[0], t: T, y: S.v.map(v => v > 0 ? v : null)});
if (D.power) battery.push({name: "battery, as the power monitor read it", colour: palette[3], t: D.power.t, y: D.power.v});
if (battery.length) lane("Battery (V)", 70, "V", battery);

if (S.fwd && S.fwd.some(v => v !== null)) {
  lane("Drive power", 70, "", [
    {name: "forward", colour: palette[0], t: T, y: S.fwd},
    {name: "strafe", colour: palette[2], t: T, y: S.str},
    {name: "turn", colour: palette[4], t: T, y: S.turn},
  ]);
}

if (D.power) {
  const series = [{name: "motors together", colour: css("--fg"), t: D.power.t, y: D.power.total}];
  D.power.names.forEach((name, i) => series.push({name, colour: palette[(i + 1) % palette.length], t: D.power.t, y: D.power.a[i]}));
  lane("Motor current (A)", 90, "A", series).zero = true;
}

if (D.loop) lane("Loop thread: blue is your own code, grey the SDK and libraries", 22, "", D.loop.slices, "blocks");

// ---- Time -------------------------------------------------------------------
const scrub = document.getElementById("scrub");
const readout = document.getElementById("readout");

function valueAt(times, ys, t) {
  const i = at(times, t);
  return i < 0 ? null : ys[i];
}

function setTime(t) {
  now = t;
  for (const L of lanes) {
    if (!L.cursor) continue;
    const x = xOf(t);
    L.cursor.setAttribute("x1", x); L.cursor.setAttribute("x2", x);
    L.cursor.setAttribute("visibility", t >= view[0] && t <= view[1] ? "visible" : "hidden");
  }
  scrub.value = D.seconds ? Math.round(t / D.seconds * 1000) : 0;

  const i = at(T, t);
  if (i >= 0 && S.x[i] !== null) {
    robot.setAttribute("transform", "translate(" + S.x[i] + "," + S.y[i] + ") rotate(" + deg(S.h[i] || 0) + ")");
    if (S.cx[i] !== null) { target.setAttribute("cx", S.cx[i]); target.setAttribute("cy", S.cy[i]); }
  }

  const parts = ["<b>" + t.toFixed(2) + " s</b>"];
  if (i >= 0) {
    parts.push("off the path <b>" + fmt(S.err[i], 2) + " in</b>");
    parts.push("at " + fmt(S.x[i], 1) + ", " + fmt(S.y[i], 1) + ", " + fmt(deg(S.h[i] || 0), 0) + "°");
    if (S.path && S.path[i] !== null && S.path[i] >= 0) parts.push("path " + (S.path[i] + 1));
    if (S.v && S.v[i] > 0) parts.push("battery <b>" + fmt(S.v[i], 2) + " V</b>");
  }
  if (D.power) {
    const a = valueAt(D.power.t, D.power.total, t);
    if (a !== null && t >= D.power.t[0]) parts.push("motors <b>" + fmt(a, 1) + " A</b>");
  }
  if (D.loop) {
    const s = D.loop.slices.find(s => s.a <= t && t < s.b);
    if (s) parts.push("in <b>" + s.n + "</b>");
  }
  readout.innerHTML = parts.join(" · ");
}

scrub.addEventListener("input", () => setTime(scrub.value / 1000 * D.seconds));

let playing = null;
document.getElementById("play").onclick = ev => {
  if (playing) { cancelAnimationFrame(playing); playing = null; ev.target.textContent = "Play"; return; }
  ev.target.textContent = "Pause";
  let last = performance.now();
  if (now >= D.seconds) now = 0;
  const tick = when => {
    now = Math.min(D.seconds, now + (when - last) / 1000); last = when;
    setTime(now);
    if (now < D.seconds) playing = requestAnimationFrame(tick);
    else { playing = null; ev.target.textContent = "Play"; }
  };
  playing = requestAnimationFrame(tick);
};

// ---- Tables -----------------------------------------------------------------
const endRows = ends.map(e => {
  const dist = Math.hypot(e.dx, e.dy);
  return "<tr><td>" + (e.path + 1) + (e.stop ? ' <span class="hint">stop</span>' : "") + "</td>" +
    '<td class="num">' + dist.toFixed(2) + ' in</td><td class="num">' + e.dx.toFixed(2) + '</td><td class="num">' + e.dy.toFixed(2) +
    '</td><td class="num">' + deg(e.dh).toFixed(1) + '°</td><td class="num">' + e.t.toFixed(1) + " s</td></tr>";
});
document.getElementById("ends").innerHTML = endRows.length
  ? "<tr><th>Path</th><th class=num>Off by</th><th class=num>x</th><th class=num>y</th><th class=num>Heading</th><th class=num>At</th></tr>" + endRows.join("")
  : '<tr><td class="hint">No path ended during the recording.</td></tr>';

if ((D.arrivals || []).length) {
  document.getElementById("arrivalsWrap").innerHTML = "<h2>Arriving at each pose</h2><table>" +
    "<tr><th>Pose</th><th class=num>Off by</th><th class=num>x</th><th class=num>y</th><th class=num>Heading</th></tr>" +
    D.arrivals.map(a => "<tr><td>" + a.pose + '</td><td class="num">' + Math.hypot(a.dx, a.dy).toFixed(2) + ' in</td><td class="num">' +
      a.dx.toFixed(2) + '</td><td class="num">' + a.dy.toFixed(2) + '</td><td class="num">' + deg(a.dh).toFixed(1) + "°</td></tr>").join("") +
    "</table>";
}

document.getElementById("foot").textContent =
  "The robot's pose is what its localizer reported. This shows how well the path was followed, not where the robot truly was: " +
  "localizer accuracy is Spin Check's and Pedro's localization tuning's job. Drawn by epsh from ElectroDromos's run file" +
  (D.power ? ", the power monitor's recording" : "") + (D.loop ? " and the loop profiler's" : "") + ".";

draw();
</script>
</body>
</html>
`
