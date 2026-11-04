<script lang="ts">
  import type { ScoreSeries } from "@flagfall/theme-sdk";

  let { series, start, end, highlight }: { series: ScoreSeries[]; start: number; end: number; highlight?: string } = $props();

  // Same geometry as the design: plot area x 60..820, y 20..270.
  const X0 = 60, X1 = 820, Y0 = 270, Y1 = 20;
  const greys = ["#EDE7F6", "#A99BC2", "#6F5F8C", "#5A4C75", "#4A3E62"];

  const maxScore = $derived(Math.max(1000, ...series.flatMap((s) => s.points.map((p) => p.score))));
  const yMax = $derived(Math.ceil(maxScore / 1000) * 1000);
  const span = $derived(Math.max(1, end - start));
  const x = (ms: number) => X0 + ((Math.min(Math.max(ms, start), end) - start) / span) * (X1 - X0);
  const y = (score: number) => Y0 - (Math.max(0, score) / yMax) * (Y0 - Y1);

  const gridLines = $derived([0, 1, 2, 3].map((i) => (yMax / 3) * i));
  const hours = $derived(span / 3_600_000);
  const tickEvery = $derived(hours <= 12 ? 2 : hours <= 36 ? 6 : 12);
  const ticks = $derived(Array.from({ length: Math.floor(hours / tickEvery) + 1 }, (_, i) => i * tickEvery));

  const lines = $derived.by(() => {
    let grey = 0;
    return series.map((s) => {
      const mine = s.teamId === highlight;
      const color = mine ? "var(--amber)" : greys[Math.min(grey++, greys.length - 1)]!;
      let last = 0;
      const pts: string[] = [];
      for (const p of s.points) {
        const px = x(new Date(p.at).getTime());
        pts.push(`${px.toFixed(1)},${y(last).toFixed(1)}`, `${px.toFixed(1)},${y(p.score).toFixed(1)}`);
        last = p.score;
      }
      pts.push(`${X1},${y(last).toFixed(1)}`);
      return { id: s.teamId, name: s.teamName, color, mine, points: pts.join(" "), endY: y(last) };
    });
  });

  // Keep end labels from overlapping.
  const labels = $derived.by(() => {
    const sorted = [...lines].sort((a, b) => a.endY - b.endY);
    let prev = -Infinity;
    return sorted.map((l) => {
      const ly = Math.max(l.endY, prev + 16);
      prev = ly;
      return { ...l, ly };
    });
  });
</script>

<svg role="img" aria-label="Score over time for {series.map((s) => s.teamName).join(', ')}" viewBox="0 0 960 300" class="chart">
  <g class="grid">
    {#each gridLines as v}
      <line x1={X0} y1={y(v)} x2={X1} y2={y(v)} /><text x="50" y={y(v) + 4} text-anchor="end">{Math.round(v)}</text>
    {/each}
  </g>
  <g class="ticks">
    {#each ticks as h}<text x={x(start + h * 3_600_000)} y="292">{h}h</text>{/each}
  </g>
  {#each lines as l (l.id)}
    <polyline points={l.points} fill="none" stroke={l.color} stroke-width={l.mine ? 3.5 : 2} stroke-linejoin="round" />
  {/each}
  <g class="clabels">
    {#each labels as l (l.id)}
      <circle cx={X1} cy={l.endY} r="4" fill={l.color} />
      <text x="832" y={l.ly + 4} fill={l.color}>{l.name}</text>
    {/each}
  </g>
</svg>
