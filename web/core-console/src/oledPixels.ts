export const OLED_WIDTH = 128;
export const OLED_HEIGHT = 32;
const STALE_MARKER_X = 124;
const STALE_MARKER_Y = 2;
const SMALL_GLYPH_W = 5;
const SMALL_GLYPH_H = 7;
const SMALL_ADVANCE = 6;
const SMALL_SECONDARY_TOP = 4; // u8g2 baseline 11 - 7 ascent
const SMALL_FOOTER_TOP = 20; // u8g2 baseline 27 - 7 ascent

// Standard 5x7 monochrome column-major font (LSB = top pixel, bit 6 = bottom).
const FONT_5X7: Record<string, readonly number[]> = {
  " ": [0x00, 0x00, 0x00, 0x00, 0x00],
  "!": [0x00, 0x00, 0x5f, 0x00, 0x00],
  '"': [0x00, 0x07, 0x00, 0x07, 0x00],
  "#": [0x14, 0x7f, 0x14, 0x7f, 0x14],
  $: [0x24, 0x2a, 0x7f, 0x2a, 0x12],
  "%": [0x23, 0x13, 0x08, 0x64, 0x62],
  "&": [0x36, 0x49, 0x55, 0x22, 0x50],
  "'": [0x00, 0x05, 0x03, 0x00, 0x00],
  "(": [0x00, 0x1c, 0x22, 0x41, 0x00],
  ")": [0x00, 0x41, 0x22, 0x1c, 0x00],
  "*": [0x14, 0x08, 0x3e, 0x08, 0x14],
  "+": [0x08, 0x08, 0x3e, 0x08, 0x08],
  ",": [0x00, 0x50, 0x30, 0x00, 0x00],
  "-": [0x08, 0x08, 0x08, 0x08, 0x08],
  ".": [0x00, 0x60, 0x60, 0x00, 0x00],
  "/": [0x20, 0x10, 0x08, 0x04, 0x02],
  "0": [0x3e, 0x51, 0x49, 0x45, 0x3e],
  "1": [0x00, 0x42, 0x7f, 0x40, 0x00],
  "2": [0x42, 0x61, 0x51, 0x49, 0x46],
  "3": [0x22, 0x41, 0x49, 0x49, 0x36],
  "4": [0x18, 0x14, 0x12, 0x7f, 0x10],
  "5": [0x27, 0x45, 0x45, 0x45, 0x39],
  "6": [0x3c, 0x4a, 0x49, 0x49, 0x30],
  "7": [0x01, 0x71, 0x09, 0x05, 0x03],
  "8": [0x36, 0x49, 0x49, 0x49, 0x36],
  "9": [0x06, 0x49, 0x49, 0x29, 0x1e],
  ":": [0x00, 0x36, 0x36, 0x00, 0x00],
  ";": [0x00, 0x56, 0x36, 0x00, 0x00],
  "<": [0x08, 0x14, 0x22, 0x41, 0x00],
  "=": [0x14, 0x14, 0x14, 0x14, 0x14],
  ">": [0x00, 0x41, 0x22, 0x14, 0x08],
  "?": [0x02, 0x01, 0x51, 0x09, 0x06],
  "@": [0x32, 0x49, 0x79, 0x41, 0x3e],
  A: [0x7e, 0x11, 0x11, 0x11, 0x7e],
  B: [0x7f, 0x49, 0x49, 0x49, 0x36],
  C: [0x3e, 0x41, 0x41, 0x41, 0x22],
  D: [0x7f, 0x41, 0x41, 0x22, 0x1c],
  E: [0x7f, 0x49, 0x49, 0x49, 0x41],
  F: [0x7f, 0x09, 0x09, 0x09, 0x01],
  G: [0x3e, 0x41, 0x49, 0x49, 0x7a],
  H: [0x7f, 0x08, 0x08, 0x08, 0x7f],
  I: [0x00, 0x41, 0x7f, 0x41, 0x00],
  J: [0x20, 0x40, 0x41, 0x3f, 0x01],
  K: [0x7f, 0x08, 0x14, 0x22, 0x41],
  L: [0x7f, 0x40, 0x40, 0x40, 0x40],
  M: [0x7f, 0x02, 0x0c, 0x02, 0x7f],
  N: [0x7f, 0x04, 0x08, 0x10, 0x7f],
  O: [0x3e, 0x41, 0x41, 0x41, 0x3e],
  P: [0x7f, 0x09, 0x09, 0x09, 0x06],
  Q: [0x3e, 0x41, 0x51, 0x21, 0x5e],
  R: [0x7f, 0x09, 0x19, 0x29, 0x46],
  S: [0x46, 0x49, 0x49, 0x49, 0x31],
  T: [0x01, 0x01, 0x7f, 0x01, 0x01],
  U: [0x3f, 0x40, 0x40, 0x40, 0x3f],
  V: [0x1f, 0x20, 0x40, 0x20, 0x1f],
  W: [0x3f, 0x40, 0x38, 0x40, 0x3f],
  X: [0x63, 0x14, 0x08, 0x14, 0x63],
  Y: [0x07, 0x08, 0x70, 0x08, 0x07],
  Z: [0x61, 0x51, 0x49, 0x45, 0x43],
  "[": [0x00, 0x7f, 0x41, 0x41, 0x00],
  "\\": [0x02, 0x04, 0x08, 0x10, 0x20],
  "]": [0x00, 0x41, 0x41, 0x7f, 0x00],
  "^": [0x04, 0x02, 0x01, 0x02, 0x04],
  _: [0x40, 0x40, 0x40, 0x40, 0x40],
  a: [0x20, 0x54, 0x54, 0x54, 0x78],
  b: [0x7f, 0x48, 0x44, 0x44, 0x38],
  c: [0x38, 0x44, 0x44, 0x44, 0x20],
  d: [0x38, 0x44, 0x44, 0x48, 0x7f],
  e: [0x38, 0x54, 0x54, 0x54, 0x18],
  f: [0x08, 0x7e, 0x09, 0x01, 0x02],
  g: [0x08, 0x54, 0x54, 0x54, 0x3c],
  h: [0x7f, 0x08, 0x04, 0x04, 0x78],
  i: [0x00, 0x44, 0x7d, 0x40, 0x00],
  j: [0x20, 0x40, 0x44, 0x3d, 0x00],
  k: [0x7f, 0x10, 0x28, 0x44, 0x00],
  l: [0x00, 0x41, 0x7f, 0x40, 0x00],
  m: [0x7c, 0x04, 0x18, 0x04, 0x78],
  n: [0x7c, 0x08, 0x04, 0x04, 0x78],
  o: [0x38, 0x44, 0x44, 0x44, 0x38],
  p: [0x7c, 0x14, 0x14, 0x14, 0x08],
  q: [0x08, 0x14, 0x14, 0x18, 0x7c],
  r: [0x7c, 0x08, 0x04, 0x04, 0x08],
  s: [0x48, 0x54, 0x54, 0x54, 0x20],
  t: [0x04, 0x3f, 0x44, 0x40, 0x20],
  u: [0x3c, 0x40, 0x40, 0x20, 0x7c],
  v: [0x1c, 0x20, 0x40, 0x20, 0x1c],
  w: [0x3c, 0x40, 0x30, 0x40, 0x3c],
  x: [0x44, 0x28, 0x10, 0x28, 0x44],
  y: [0x0c, 0x50, 0x50, 0x50, 0x3c],
  z: [0x44, 0x64, 0x54, 0x4c, 0x44],
};

// Bold 18px-tall primary OLED bitmap glyphs matching u8g2_font_helvB18 proportions.
const PRIMARY_GLYPHS: Record<string, readonly string[]> = {
  " ": [
    "....",
    "....",
    "....",
    "....",
    "....",
    "....",
    "....",
    "....",
    "....",
    "....",
    "....",
    "....",
    "....",
    "....",
    "....",
    "....",
    "....",
    "....",
  ],
  ".": [
    "...",
    "...",
    "...",
    "...",
    "...",
    "...",
    "...",
    "...",
    "...",
    "...",
    "...",
    "...",
    "...",
    "...",
    "...",
    "###",
    "###",
    "###",
  ],
  ":": [
    "...",
    "...",
    "...",
    "...",
    "###",
    "###",
    "###",
    "...",
    "...",
    "...",
    "...",
    "###",
    "###",
    "###",
    "...",
    "...",
    "...",
    "...",
  ],
  "-": [
    "........",
    "........",
    "........",
    "........",
    "........",
    "........",
    "........",
    "########",
    "########",
    "########",
    "........",
    "........",
    "........",
    "........",
    "........",
    "........",
    "........",
    "........",
  ],
  "+": [
    "........",
    "........",
    "........",
    "........",
    "...##...",
    "...##...",
    "...##...",
    "...##...",
    "########",
    "########",
    "...##...",
    "...##...",
    "...##...",
    "...##...",
    "........",
    "........",
    "........",
    "........",
  ],
  "/": [
    ".......##",
    "......##.",
    "......##.",
    ".....##..",
    ".....##..",
    "....##...",
    "....##...",
    "...##....",
    "...##....",
    "...##....",
    "..##.....",
    "..##.....",
    ".##......",
    ".##......",
    "##.......",
    "##.......",
    "##.......",
    ".........",
  ],
  "0": [
    "..######..",
    ".########.",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    ".########.",
    "..######..",
  ],
  "1": [
    "....###",
    "...####",
    "..#####",
    ".######",
    "###.###",
    "....###",
    "....###",
    "....###",
    "....###",
    "....###",
    "....###",
    "....###",
    "....###",
    "....###",
    "....###",
    "....###",
    "....###",
    "....###",
  ],
  "2": [
    ".#######..",
    "#########.",
    "###...####",
    ".......###",
    ".......###",
    ".......###",
    "......###.",
    ".....###..",
    "....###...",
    "...###....",
    "..###.....",
    ".###......",
    "###.......",
    "###.......",
    "###.......",
    "##########",
    "##########",
    "##########",
  ],
  "3": [
    ".#######..",
    "#########.",
    "###...####",
    ".......###",
    ".......###",
    ".......###",
    "......###.",
    "..######..",
    "..######..",
    "......###.",
    ".......###",
    ".......###",
    ".......###",
    ".......###",
    "###...####",
    "#########.",
    ".#######..",
    "..........",
  ],
  "4": [
    ".....####.",
    "....#####.",
    "...######.",
    "..###.###.",
    ".###..###.",
    "###...###.",
    "###...###.",
    "###...###.",
    "###...###.",
    "##########",
    "##########",
    "......###.",
    "......###.",
    "......###.",
    "......###.",
    "......###.",
    "......###.",
    "......###.",
  ],
  "5": [
    "#########.",
    "#########.",
    "###.......",
    "###.......",
    "###.......",
    "########..",
    "#########.",
    "......####",
    ".......###",
    ".......###",
    ".......###",
    ".......###",
    ".......###",
    "###....###",
    "###...####",
    ".########.",
    "..######..",
    "..........",
  ],
  "6": [
    "..######..",
    ".########.",
    "###....###",
    "###.......",
    "###.......",
    "###.......",
    "########..",
    "#########.",
    "###...####",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    ".########.",
    "..######..",
    "..........",
  ],
  "7": [
    "##########",
    "##########",
    ".......###",
    "......###.",
    "......###.",
    ".....###..",
    ".....###..",
    "....###...",
    "....###...",
    "...###....",
    "...###....",
    "..###.....",
    "..###.....",
    "..###.....",
    "..###.....",
    "..###.....",
    "..###.....",
    "..........",
  ],
  "8": [
    "..######..",
    ".########.",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    ".########.",
    "..######..",
    ".########.",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    ".########.",
    "..######..",
    "..........",
  ],
  "9": [
    "..######..",
    ".########.",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###....###",
    "###...####",
    ".#########",
    "..########",
    ".......###",
    ".......###",
    ".......###",
    ".......###",
    "###....###",
    ".########.",
    "..######..",
    "..........",
  ],
  "%": [
    ".###.......##.",
    "#####.....##..",
    "##.##....##...",
    "#####...##....",
    ".###...##.....",
    "......##......",
    ".....##.......",
    "....##........",
    "...##.........",
    "..##..........",
    ".##......###..",
    "##......#####.",
    "........##.##.",
    "........#####.",
    ".........###..",
    "..............",
    "..............",
    "..............",
  ],
  $: [
    "....##....",
    "....##....",
    ".#######..",
    "#########.",
    "###.##.###",
    "###.##....",
    ".######...",
    "..#######.",
    "....##.###",
    "....##.###",
    "###.##.###",
    "#########.",
    ".#######..",
    "....##....",
    "....##....",
    "..........",
    "..........",
    "..........",
  ],
};

function setPixel(buf: Uint8Array, x: number, y: number) {
  if (x < 0 || x >= OLED_WIDTH || y < 0 || y >= OLED_HEIGHT) return;
  buf[y * OLED_WIDTH + x] = 1;
}

function drawSmallGlyph(
  buf: Uint8Array,
  ch: string,
  originX: number,
  originY: number,
  scale = 1,
) {
  const cols = FONT_5X7[ch] ?? FONT_5X7["?"];
  for (let col = 0; col < SMALL_GLYPH_W; col++) {
    const bits = cols[col];
    for (let row = 0; row < SMALL_GLYPH_H; row++) {
      if ((bits & (1 << row)) === 0) continue;
      for (let sy = 0; sy < scale; sy++) {
        for (let sx = 0; sx < scale; sx++) {
          setPixel(buf, originX + col * scale + sx, originY + row * scale + sy);
        }
      }
    }
  }
}

function drawSmallText(buf: Uint8Array, text: string, x: number, topY: number) {
  let cursor = x;
  for (const ch of text) {
    drawSmallGlyph(buf, ch, cursor, topY, 1);
    cursor += SMALL_ADVANCE;
  }
}

function primaryGlyphWidth(ch: string): number {
  const glyph = PRIMARY_GLYPHS[ch];
  if (glyph) return glyph[0].length;
  return SMALL_GLYPH_W * 2;
}

function measurePrimaryWidth(text: string): number {
  if (!text) return 0;
  let total = 0;
  for (let i = 0; i < text.length; i++) {
    if (i > 0) total += 2;
    total += primaryGlyphWidth(text[i]);
  }
  return total;
}

function drawPrimaryText(
  buf: Uint8Array,
  text: string,
  startX: number,
  topY: number,
) {
  let cursor = startX;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    const glyph = PRIMARY_GLYPHS[ch];
    if (glyph) {
      for (let r = 0; r < glyph.length; r++) {
        const line = glyph[r];
        for (let c = 0; c < line.length; c++) {
          if (line[c] === "#") setPixel(buf, cursor + c, topY + r);
        }
      }
      cursor += glyph[0].length + 2;
    } else {
      drawSmallGlyph(buf, ch, cursor, topY + 2, 2);
      cursor += SMALL_GLYPH_W * 2 + 2;
    }
  }
}

function splitCountdown(
  text: string,
  separator: string,
): { count: string; reset: string } | null {
  if (text.length !== 8 || text[2] !== " " || text[5] !== separator)
    return null;
  for (let i = 0; i < 8; i++) {
    if (i === 2 || i === 5) continue;
    const ch = text[i];
    if ((ch >= "0" && ch <= "9") || ch === "-") continue;
    if (i === 1 && ch === " ") continue;
    return null;
  }
  const count = text[1] === " " ? text[0] : text.slice(0, 2);
  return { count, reset: text.slice(3) };
}

function fitSmall(text: string, maxWidth: number): string {
  let out = text.slice(0, 64);
  while (out.length > 0 && out.length * SMALL_ADVANCE > maxWidth) {
    out = out.slice(0, -1);
  }
  return out;
}

function fitPrimary(text: string, maxWidth: number): string {
  let out = text.slice(0, 64);
  while (out.length > 0 && measurePrimaryWidth(out) > maxWidth) {
    out = out.slice(0, -1);
  }
  return out;
}

export function renderOledBuffer(input: {
  primary: string;
  secondary: string;
  footer: string;
  stale: boolean;
}): Uint8Array {
  const buf = new Uint8Array(OLED_WIDTH * OLED_HEIGHT);
  const rawPrimary = input.primary || "";
  const rawSecondary = input.secondary || "";
  const rawFooter = input.footer || "";

  const daySplit = splitCountdown(rawSecondary, "/");
  const hourSplit = splitCountdown(rawFooter, ":");
  const countdown =
    rawPrimary.endsWith("%") && daySplit !== null && hourSplit !== null;

  const secondaryText = countdown ? daySplit.reset : rawSecondary;
  const footerText = countdown ? hourSplit.reset : rawFooter;

  const secFitted = fitSmall(secondaryText, OLED_WIDTH);
  const footFitted = fitSmall(footerText, OLED_WIDTH);
  const secW = secFitted.length * SMALL_ADVANCE;
  const footW = footFitted.length * SMALL_ADVANCE;
  const rightW = Math.max(secW, footW);
  const leftW = countdown ? 2 * SMALL_ADVANCE : 0;
  const primaryAreaW = Math.max(0, OLED_WIDTH - leftW - rightW);

  const primaryFitted = fitPrimary(
    rawPrimary,
    countdown ? primaryAreaW : OLED_WIDTH,
  );
  const primaryW = measurePrimaryWidth(primaryFitted);
  const primaryX =
    primaryW < primaryAreaW
      ? leftW + Math.floor((primaryAreaW - primaryW) / 2)
      : leftW;
  drawPrimaryText(buf, primaryFitted, primaryX, 7);

  if (countdown) {
    drawSmallText(buf, daySplit.count, 0, SMALL_SECONDARY_TOP);
    drawSmallText(buf, hourSplit.count, 0, SMALL_FOOTER_TOP);
  }
  drawSmallText(
    buf,
    secFitted,
    secW < OLED_WIDTH ? OLED_WIDTH - secW : 0,
    SMALL_SECONDARY_TOP,
  );
  drawSmallText(
    buf,
    footFitted,
    footW < OLED_WIDTH ? OLED_WIDTH - footW : 0,
    SMALL_FOOTER_TOP,
  );

  if (input.stale) {
    setPixel(buf, STALE_MARKER_X, STALE_MARKER_Y);
    setPixel(buf, STALE_MARKER_X + 1, STALE_MARKER_Y);
    setPixel(buf, STALE_MARKER_X, STALE_MARKER_Y + 1);
    setPixel(buf, STALE_MARKER_X + 1, STALE_MARKER_Y + 1);
  }
  return buf;
}
