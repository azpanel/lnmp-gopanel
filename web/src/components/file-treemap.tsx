import { useState } from "react";
import { ResponsiveContainer, Treemap, type TreemapNode } from "recharts";
import { bytes } from "@/lib/format";
import type { LargeFile } from "@/lib/types";

export const fileColors = [
  "#489c8d",
  "#658cbb",
  "#8b7cb5",
  "#ae8662",
  "#6b9b6f",
  "#b06e7f",
  "#5f9ca8",
  "#9b9467",
];
export function FileTreemap({
  files,
  hideSwapfile = false,
}: {
  files: LargeFile[];
  hideSwapfile?: boolean;
}) {
  const [selected, setSelected] = useState<string | null>(null);
  const visibleFiles = files.filter(
    (f) =>
      f.size > 0 && (!hideSwapfile || f.path.split("/").at(-1) !== "swapfile"),
  );
  const total = visibleFiles.reduce((sum, f) => sum + f.size, 0);
  const data = visibleFiles.map((f) => ({
    name: f.path,
    size: f.size,
    rank: files.indexOf(f),
  }));
  const active = visibleFiles.find((f) => f.path === selected);
  function content(node: TreemapNode) {
    if (node.depth !== 1) return <g />;
    const rank = Number(node.rank ?? node.index);
    const path = String(node.name);
    const name = path.split("/").at(-1) || path;
    const selected = active?.path === path;
    const width = Math.max(0, node.width - 3),
      height = Math.max(0, node.height - 3);
    const maxCharacters = Math.max(0, Math.floor((width - 24) / 9));
    const label =
      name.length > maxCharacters
        ? `${name.slice(0, Math.max(0, maxCharacters - 1))}…`
        : name;
    return (
      <g
        className="file-tile"
        tabIndex={0}
        role="button"
        aria-label={`${path}，${bytes(node.value)}`}
        onClick={() => setSelected(path)}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            setSelected(path);
          }
        }}
      >
        <title>
          {path}
          {"\n"}
          {bytes(node.value)} ·{" "}
          {total ? ((node.value / total) * 100).toFixed(2) : 0}%
        </title>
        <rect
          x={node.x + 1.5}
          y={node.y + 1.5}
          width={width}
          height={height}
          rx={4}
          fill={fileColors[rank % fileColors.length]}
          fillOpacity={selected ? 1 : 0.8}
          stroke={selected ? "#e3fff6" : "transparent"}
          strokeWidth={selected ? 2 : 0}
        />
        {width > 70 && height > 42 && (
          <text
            x={node.x + 12}
            y={node.y + 23}
            fill="#fff"
            fontSize={15}
            fontWeight={500}
            pointerEvents="none"
          >
            {label}
          </text>
        )}
        {width > 105 && height > 68 && (
          <text
            x={node.x + 12}
            y={node.y + 44}
            fill="#ffffffc9"
            fontSize={14}
            fontFamily="monospace"
            pointerEvents="none"
          >
            {bytes(node.value)}
          </text>
        )}
        {width > 115 && height > 100 && (
          <text
            x={node.x + 12}
            y={node.y + height - 13}
            fill="#ffffffb0"
            fontSize={14}
            pointerEvents="none"
          >
            {((node.value / total) * 100).toFixed(1)}%
          </text>
        )}
      </g>
    );
  }
  return (
    <>
      <div
        id="storage-file-treemap"
        className="file-treemap"
        data-testid="file-treemap"
      >
        {total > 0 ? (
          <ResponsiveContainer width="100%" height="100%">
            <Treemap
              data={data}
              dataKey="size"
              nameKey="name"
              aspectRatio={1}
              isAnimationActive={false}
              content={content}
            />
          </ResponsiveContainer>
        ) : (
          <div className="treemap-empty">暂无可绘制的文件大小数据</div>
        )}
      </div>
      <div className="treemap-selection">
        {active ? (
          <>
            <span className="font-mono break-all">{active.path}</span>
            <strong>
              {bytes(active.size)} · {((active.size / total) * 100).toFixed(2)}%
            </strong>
          </>
        ) : (
          <span>
            悬停查看完整路径，点击矩形选中文件。面积表示该文件占图中显示文件合计大小的比例。
          </span>
        )}
      </div>
    </>
  );
}
