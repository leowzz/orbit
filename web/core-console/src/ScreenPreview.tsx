import { useEffect, useRef } from "react";
import type { NodePreview } from "./api";
import { OLED_HEIGHT, OLED_WIDTH, renderOledBuffer } from "./oledPixels";

const XIAOZHI_WIDTH = 320;
const XIAOZHI_HEIGHT = 240;

type Props = {
  profile: string;
  preview: NodePreview;
  brightnessPercent?: number;
};

function kindLabel(displayKind: string): string {
  if (displayKind === "system") return "系统资源";
  if (displayKind === "codex") return "账号周限";
  if (displayKind === "usage") return "API 用量";
  return "默认";
}

function OledPixelCanvas({
  preview,
  brightnessPercent,
}: {
  preview: NodePreview;
  brightnessPercent?: number;
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const off = brightnessPercent === 0;
  const stale = preview.freshness !== "fresh";

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const dpr =
      typeof window !== "undefined" ? window.devicePixelRatio || 1 : 1;
    const scale = dpr >= 1.5 ? 4 : 2;
    const w = OLED_WIDTH * scale;
    const h = OLED_HEIGHT * scale;
    if (canvas.width !== w) canvas.width = w;
    if (canvas.height !== h) canvas.height = h;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    ctx.fillStyle = "#070a0c";
    ctx.fillRect(0, 0, w, h);

    const buffer = renderOledBuffer({
      primary: preview.primary,
      secondary: preview.secondary,
      footer: preview.footer,
      stale,
    });
    const alpha = off
      ? 0.16
      : brightnessPercent === undefined
        ? 0.96
        : 0.36 + 0.64 * (brightnessPercent / 100);
    ctx.fillStyle = `rgba(236, 247, 241, ${alpha.toFixed(2)})`;
    const dotSize = scale >= 4 ? 3 : 2;

    for (let y = 0; y < OLED_HEIGHT; y++) {
      for (let x = 0; x < OLED_WIDTH; x++) {
        if (buffer[y * OLED_WIDTH + x]) {
          ctx.fillRect(x * scale, y * scale, dotSize, dotSize);
        }
      }
    }
  }, [
    preview.primary,
    preview.secondary,
    preview.footer,
    stale,
    off,
    brightnessPercent,
  ]);

  return (
    <canvas
      ref={canvasRef}
      width={OLED_WIDTH * 2}
      height={OLED_HEIGHT * 2}
      className="screen-preview-canvas screen-preview-oled"
      aria-label={`OLED 128×32 像素预览：${preview.primary || "--"} ${preview.secondary || ""} ${preview.footer || ""}`}
    />
  );
}

function XiaozhiPixelCanvas({
  preview,
  brightnessPercent,
}: {
  preview: NodePreview;
  brightnessPercent?: number;
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const off = brightnessPercent === 0;
  const stale = preview.freshness !== "fresh";

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    ctx.fillStyle = "#080c08";
    ctx.fillRect(0, 0, XIAOZHI_WIDTH, XIAOZHI_HEIGHT);

    const alpha = off
      ? 0.16
      : brightnessPercent === undefined
        ? 1
        : 0.36 + 0.64 * (brightnessPercent / 100);
    ctx.save();
    ctx.globalAlpha = alpha;

    ctx.fillStyle = stale ? "#ffa400" : "#50fc50";
    ctx.beginPath();
    ctx.arc(19, 15, 3, 0, Math.PI * 2);
    ctx.fill();

    ctx.strokeStyle = "#292d29";
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.moveTo(16, 160);
    ctx.lineTo(304, 160);
    ctx.stroke();

    ctx.fillStyle = stale ? "#ffa400" : "#ffffff";
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    ctx.font =
      "700 64px ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace";
    ctx.fillText(preview.primary || "--", 160, 96, 288);

    ctx.fillStyle = "#ffffff";
    ctx.font =
      "500 20px ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace";
    ctx.textBaseline = "alphabetic";
    ctx.textAlign = "left";
    ctx.fillText(preview.secondary || "--", 16, 204, 168);
    ctx.textAlign = "right";
    ctx.fillText(preview.footer || "--", 304, 204, 116);
    ctx.restore();
  }, [
    preview.primary,
    preview.secondary,
    preview.footer,
    stale,
    off,
    brightnessPercent,
  ]);

  return (
    <canvas
      ref={canvasRef}
      width={XIAOZHI_WIDTH}
      height={XIAOZHI_HEIGHT}
      className="screen-preview-canvas screen-preview-xiaozhi"
      aria-label={`小智 320×240 像素预览：${preview.primary || "--"} ${preview.secondary || ""} ${preview.footer || ""}`}
    />
  );
}

export default function ScreenPreview({
  profile,
  preview,
  brightnessPercent,
}: Props) {
  const isOled = profile.endsWith("-oled-128x32");
  const isXiaozhi = profile === "metrics-xiaozhi-320x240";
  const status = [
    preview.rotation_total && preview.rotation_total > 1
      ? `${preview.rotation_step}/${preview.rotation_total}`
      : "",
    isOled ? "128×32" : isXiaozhi ? "320×240" : kindLabel(preview.display_kind),
    brightnessPercent === 0
      ? "已息屏"
      : preview.freshness === "fresh"
        ? "新鲜"
        : "已过期",
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <div className="screen-preview" aria-label="当前屏幕文本预览">
      <div className="screen-preview-head">
        <span>当前渲染输出</span>
        <small>{status}</small>
      </div>
      {isOled ? (
        <OledPixelCanvas
          preview={preview}
          brightnessPercent={brightnessPercent}
        />
      ) : isXiaozhi ? (
        <XiaozhiPixelCanvas
          preview={preview}
          brightnessPercent={brightnessPercent}
        />
      ) : (
        <div className="screen-preview-slots">
          <strong className="screen-preview-primary">
            {preview.primary || "--"}
          </strong>
          <div className="screen-preview-meta">
            <span>{preview.secondary || "--"}</span>
            <span>{preview.footer || "--"}</span>
          </div>
        </div>
      )}
    </div>
  );
}
