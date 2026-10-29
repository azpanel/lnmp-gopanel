import { useEffect, useState } from "react";
import { HardDrive, Pause, Play } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { FileTreemap } from "@/components/file-treemap";
import { Meter } from "@/components/meter";
import { MetricChart } from "@/components/metric-chart";
import { ServiceIcon, ServiceStatus, serviceInfo } from "@/components/status";
import type { Snapshot } from "@/lib/types";

const slides = [
  {
    id: "resources",
    name: "资源趋势",
    title: "运行状态，",
    accent: "一目了然。",
    description: ["实时观察 CPU 与内存的变化，", "掌握服务器的每一次心跳。"],
  },
  {
    id: "services",
    name: "服务状态",
    title: "核心服务，",
    accent: "尽在掌握。",
    description: ["从 Web 服务到数据库，", "集中查看每一项服务的运行状态。"],
  },
  {
    id: "storage",
    name: "磁盘空间",
    title: "磁盘空间，",
    accent: "心中有数。",
    description: ["看清磁盘使用与文件占用，", "为每一次增长留出充足空间。"],
  },
];

// Fixed examples are independent of the host and require no authenticated APIs.
const samples: Snapshot[] = Array.from({ length: 61 }, (_, index) => ({
  timestamp: new Date(Date.UTC(2026, 0, 1, 2, 0, index * 5)).toISOString(),
  cpu: Math.round(24 + 11 * Math.sin(index / 5) + 6 * Math.cos(index / 3)),
  memory: {
    used: 3.2 * 1024 ** 3,
    total: 8 * 1024 ** 3,
    percent: 40 + 3 * Math.sin(index / 9),
  },
  swap: null,
  load: null,
  uptime: null,
  network: null,
  disk_io: null,
  disks: [],
  errors: {},
}));
const files = [
  { path: "/demo/media", size: 18 * 1024 ** 3 },
  { path: "/demo/database", size: 12 * 1024 ** 3 },
  { path: "/demo/backups", size: 8 * 1024 ** 3 },
  { path: "/demo/assets", size: 4 * 1024 ** 3 },
];

function useMedia(query: string) {
  const [matches, setMatches] = useState(
    () => window.matchMedia(query).matches,
  );
  useEffect(() => {
    const media = window.matchMedia(query);
    const update = () => setMatches(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, [query]);
  return matches;
}

function Demo({ theme }: { theme: string }) {
  if (theme === "resources") return <MetricChart samples={samples} />;
  if (theme === "services") {
    return (
      <Card className="login-demo-card">
        <CardHeader>
          <CardTitle>LNMP 服务</CardTitle>
        </CardHeader>
        <CardContent className="login-demo-services">
          {["nginx", "php-fpm", "mysql", "redis"].map((name) => (
            <div className="login-demo-service" key={name}>
              <ServiceIcon name={name} />
              <span>{serviceInfo[name].title}</span>
              <ServiceStatus state="running" />
            </div>
          ))}
        </CardContent>
      </Card>
    );
  }
  return (
    <Card className="login-demo-card">
      <CardHeader className="login-demo-disk-heading">
        <CardTitle>
          <HardDrive size={16} />
          磁盘空间
        </CardTitle>
        <span>42 / 100 GiB</span>
      </CardHeader>
      <CardContent className="login-demo-storage">
        <Meter value={42} />
        <FileTreemap files={files} hint="点击色块查看示例文件占用" />
      </CardContent>
    </Card>
  );
}

export function LoginShowcase() {
  const desktop = useMedia("(min-width: 721px)");
  const reducedMotion = useMedia("(prefers-reduced-motion: reduce)");
  const [active, setActive] = useState(0);
  const [playbackPaused, setPlaybackPaused] = useState<boolean | null>(null);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [visible, setVisible] = useState(() => !document.hidden);
  const [selection, setSelection] = useState(0);
  const paused = playbackPaused ?? reducedMotion;

  useEffect(() => {
    const update = () => setVisible(!document.hidden);
    document.addEventListener("visibilitychange", update);
    return () => document.removeEventListener("visibilitychange", update);
  }, []);

  useEffect(() => {
    if (paused || hovered || focused || !visible) return;
    const timer = window.setTimeout(
      () => setActive((index) => (index + 1) % slides.length),
      5000,
    );
    return () => window.clearTimeout(timer);
  }, [active, paused, hovered, focused, visible, selection]);

  return (
    <section
      className="login-intro"
      aria-label="功能演示"
      aria-roledescription="轮播"
      aria-live="off"
      onPointerEnter={(event) => {
        if (event.pointerType === "mouse") setHovered(true);
      }}
      onPointerLeave={() => setHovered(false)}
      onFocusCapture={() => setFocused(true)}
      onBlurCapture={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget))
          setFocused(false);
      }}
    >
      <div className="eyebrow">
        <span className="live-dot" />
        为你的服务器而生
      </div>
      <div className="login-showcase-stage">
        {slides.map((slide, index) => (
          <div
            className="login-slide"
            key={slide.id}
            data-active={index === active}
            data-theme={slide.id}
            role="group"
            aria-roledescription="幻灯片"
            aria-label={`${index + 1} / ${slides.length} · ${slide.name}`}
            aria-hidden={index !== active}
            inert={index !== active}
          >
            <h1>
              {slide.title}
              <br />
              <span>{slide.accent}</span>
            </h1>
            <p>
              {slide.description[0]}
              <br />
              {slide.description[1]}
            </p>
            {desktop && (
              <div className="login-demo" aria-label={`${slide.name}演示数据`}>
                <div className="login-demo-label">
                  <span>功能预览</span>
                  <span>演示数据</span>
                </div>
                <Demo theme={slide.id} />
              </div>
            )}
          </div>
        ))}
      </div>
      <div
        className="login-showcase-controls"
        role="group"
        aria-label="轮换控制"
      >
        {slides.map((slide, index) => (
          <button
            type="button"
            className="login-showcase-dot"
            key={slide.id}
            aria-label={`显示${slide.name}`}
            aria-pressed={index === active}
            onClick={() => {
              setActive(index);
              setSelection((value) => value + 1);
            }}
          >
            <span />
          </button>
        ))}
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          className="login-showcase-pause"
          aria-label={paused ? "继续自动轮换" : "暂停自动轮换"}
          title={paused ? "继续自动轮换" : "暂停自动轮换"}
          onClick={() => setPlaybackPaused(!paused)}
        >
          {paused ? <Play /> : <Pause />}
        </Button>
      </div>
    </section>
  );
}
