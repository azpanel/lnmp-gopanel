import { Button } from "@/components/ui/button";
import { LoadBoundary } from "@/components/load-boundary";
import { Pause, Play } from "lucide-react";
import { lazy, Suspense, useEffect, useState } from "react";
const Demo = lazy(() => import("@/components/login-demo"));
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
  {
    id: "network",
    name: "网络吞吐",
    title: "网络流量，",
    accent: "清晰可见。",
    description: ["实时掌握接收与发送速率，", "看清每一次流量变化。"],
  },
  {
    id: "logs",
    name: "日志速览",
    title: "关键日志，",
    accent: "随时可查。",
    description: ["从访问记录到运行日志，", "快速找到排查问题的线索。"],
  },
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
            {desktop && index === active && (
              <div className="login-demo" aria-label={`${slide.name}演示数据`}>
                <LoadBoundary
                  fallback={
                    <div className="login-demo-card p-6" role="status">
                      演示暂不可用
                    </div>
                  }
                >
                  <Suspense
                    fallback={
                      <div
                        className="login-demo-card"
                        role="status"
                        aria-label="正在加载演示"
                      />
                    }
                  >
                    <Demo theme={slide.id} />
                  </Suspense>
                </LoadBoundary>
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
