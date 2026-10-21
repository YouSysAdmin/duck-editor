# Runs a command in a virtual terminal, feeds keys and renders the final
# screen as an SVG terminal window, used for the README screenshots.
#
#   OUT=docs/images/x.svg STEPS='[["keys", seconds], ...]' python3 hack/screenshot.py bin/dk file
#
# Needs the pyte package. COLS, ROWS and START (seconds before the first
# key) are optional, FRAME=0 leaves out the window chrome for a screen that
# sits in a frame of its own, such as the hero of the documentation site.
# task screenshots runs it with a clean config.
import fcntl
import html
import json
import os
import pty
import select
import struct
import sys
import termios
import time

import pyte

COLS, ROWS = int(os.environ.get("COLS", 120)), int(os.environ.get("ROWS", 34))
out = os.environ["OUT"]
steps = json.loads(os.environ.get("STEPS", "[]"))
cmd = sys.argv[1:]


class Screen(pyte.Screen):
    # SU and SD scroll the region without moving the cursor.
    def scroll_up(self, count=None, *args, **kwargs):
        cursor = (self.cursor.x, self.cursor.y)
        top, bottom = self.margins or pyte.screens.Margins(0, self.lines - 1)
        self.cursor.y = bottom
        for _ in range(count or 1):
            self.index()
        self.cursor.x, self.cursor.y = cursor

    def scroll_down(self, count=None, *args, **kwargs):
        cursor = (self.cursor.x, self.cursor.y)
        top, bottom = self.margins or pyte.screens.Margins(0, self.lines - 1)
        self.cursor.y = top
        for _ in range(count or 1):
            self.reverse_index()
        self.cursor.x, self.cursor.y = cursor


class Stream(pyte.ByteStream):
    csi = dict(pyte.ByteStream.csi, S="scroll_up", T="scroll_down")


screen = Screen(COLS, ROWS)
stream = Stream(screen)
pid, fd = pty.fork()
if pid == 0:
    os.environ["TERM"] = "xterm-256color"
    os.environ["COLORTERM"] = "truecolor"
    os.execvp(cmd[0], cmd)
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))


def pump(t):
    end = time.time() + t
    while time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.05)
        if r:
            try:
                data = os.read(fd, 65536)
            except OSError:
                return
            stream.feed(data)


pump(float(os.environ.get("START", "3")))
for keys, wait in steps:
    os.write(fd, keys.encode())
    pump(wait)


# A resize makes the program redraw the whole screen, so the capture does
# not depend on how well the emulator follows its partial updates.
def resize(cols, rows):
    screen.resize(rows, cols)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
    os.kill(pid, 28)


resize(COLS - 1, ROWS)
pump(0.7)
screen.reset()
resize(COLS, ROWS)
pump(1.0)

# Default colors of the terminal around the editor.
FG, BG = "cdd6f4", "1e1e2e"
NAMES = {
    "black": "45475a",
    "red": "f38ba8",
    "green": "a6e3a1",
    "brown": "f9e2af",
    "yellow": "f9e2af",
    "blue": "89b4fa",
    "magenta": "f5c2e7",
    "cyan": "94e2d5",
    "white": "bac2de",
}


def color(c, default):
    if c == "default":
        return default
    if c in NAMES:
        return NAMES[c]
    if len(c) == 6:
        return c
    return default


CW, CH, FS = 8.4, 18.0, 14
FRAME = os.environ.get("FRAME", "1") != "0"
PAD, TOP = 16, 36 if FRAME else 16
W = PAD * 2 + COLS * CW
H = TOP + PAD + ROWS * CH
parts = [
    f'<svg xmlns="http://www.w3.org/2000/svg" xml:space="preserve" width="{W:.0f}" height="{H:.0f}" viewBox="0 0 {W:.0f} {H:.0f}">',
    '<style>text{font-family:"JetBrains Mono","SF Mono",Menlo,Consolas,monospace;font-size:%dpx;white-space:pre}</style>'
    % FS,
    f'<rect width="{W:.0f}" height="{H:.0f}" rx="{10 if FRAME else 0}" fill="#{BG}"/>',
]
if FRAME:
    parts.append(
        '<circle cx="20" cy="18" r="6" fill="#f38ba8"/><circle cx="40" cy="18" r="6" fill="#f9e2af"/><circle cx="60" cy="18" r="6" fill="#a6e3a1"/>'
    )

for y in range(ROWS):
    row = screen.buffer[y]
    cells = []
    for x in range(COLS):
        ch = row[x]
        fg, bg = color(ch.fg, FG), color(ch.bg, BG)
        if ch.reverse:
            fg, bg = bg, fg
        cells.append((ch.data or " ", fg, bg, ch.bold, ch.italics, ch.underscore))
    ty = TOP + y * CH
    # backgrounds
    x = 0
    while x < COLS:
        bg = cells[x][2]
        x2 = x
        while x2 < COLS and cells[x2][2] == bg:
            x2 += 1
        if bg != BG:
            parts.append(
                f'<rect x="{PAD + x * CW:.1f}" y="{ty:.1f}" width="{(x2 - x) * CW + 0.5:.1f}" height="{CH + 0.5:.1f}" fill="#{bg}"/>'
            )
        x = x2
    # text runs
    x = 0
    while x < COLS:
        key = cells[x][1:2] + cells[x][3:]
        x2 = x
        while x2 < COLS and cells[x2][1:2] + cells[x2][3:] == key:
            x2 += 1
        text = "".join(c[0] for c in cells[x:x2])
        if text.strip():
            fg, bold, ital, und = key
            attrs = f'fill="#{fg}"'
            if bold:
                attrs += ' font-weight="bold"'
            if ital:
                attrs += ' font-style="italic"'
            if und:
                attrs += ' text-decoration="underline"'
            parts.append(
                f'<text x="{PAD + x * CW:.1f}" y="{ty + CH * 0.75:.1f}" {attrs} textLength="{(x2 - x) * CW:.1f}" lengthAdjust="spacingAndGlyphs">{html.escape(text)}</text>'
            )
        x = x2

parts.append("</svg>")
open(out, "w").write("\n".join(parts))
os.write(fd, b"\x1b")
pump(0.3)
os.write(fd, b":q!\r")
pump(0.5)
