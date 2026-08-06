#!/usr/bin/env python3
"""Turn raw Whetstone screenshots into the annotated figures used in the README.

Raw captures live in images/raw/ (full browser window). For each figure we
crop the browser chrome away, draw numbered arrows at the UI elements that
carry the argument, and append a legend band explaining each one.

Two accent colours, and the distinction is the point of the project:

    amber  what the tool does, and what it refuses to do
    green  what you wrote — notes, citations, reasons, your share of the words

Coordinates in FIGURES are given in the space the screenshots were reviewed
in (2000x1052 logical), and scaled to the native 2870x1510 capture, so they
read the same way as the UI does. Run from the repo root:

    python scripts/annotate_screenshots.py
"""

from __future__ import annotations

import math
import os

from PIL import Image, ImageDraw, ImageFont

RAW = os.path.join("images", "raw")
OUT = "images"

# Screenshots are 2870x1510; they were laid out against a 2000px-wide view.
SCALE = 2870 / 2000
# Everything above this row is browser chrome: tabs, the URL with the session
# token in it, and a personal bookmarks bar. None of it belongs in a README.
CROP_TOP = 240

AMBER = (255, 176, 32)
GREEN = (61, 220, 151)
BAND_BG = (13, 15, 20)
BAND_FG = (223, 228, 236)
BAND_DIM = (150, 158, 172)

R = 40          # marker circle radius
LINE_W = 7      # arrow shaft
HEAD_L = 42     # arrowhead length
HEAD_W = 22     # arrowhead half-width

FONT_DIR = os.path.join(os.environ.get("WINDIR", r"C:\Windows"), "Fonts")


def font(name: str, size: int) -> ImageFont.FreeTypeFont:
    """Load a system font, falling back to PIL's default if it is missing."""
    try:
        return ImageFont.truetype(os.path.join(FONT_DIR, name), size)
    except OSError:
        return ImageFont.load_default()


F_TITLE = font("segoeuib.ttf", 62)
F_LEAD = font("segoeui.ttf", 46)
F_LEGEND = font("segoeui.ttf", 44)
F_LEGEND_B = font("segoeuib.ttf", 44)
F_MARK = font("segoeuib.ttf", 46)


def pt(x: float, y: float) -> tuple[float, float]:
    """Map a reviewed-layout point onto the cropped native image."""
    return x * SCALE, y * SCALE - CROP_TOP


def arrow(d: ImageDraw.ImageDraw, tail, tip, colour) -> None:
    """Draw a shaft from just outside the marker circle to an arrowhead at tip."""
    tx, ty = pt(*tail)
    px, py = pt(*tip)
    dx, dy = px - tx, py - ty
    dist = math.hypot(dx, dy) or 1.0
    ux, uy = dx / dist, dy / dist

    start = (tx + ux * (R + 6), ty + uy * (R + 6))
    base = (px - ux * HEAD_L, py - uy * HEAD_L)
    d.line([start, base], fill=colour, width=LINE_W)
    # Perpendicular, for the two back corners of the head.
    nx, ny = -uy, ux
    d.polygon(
        [
            (px, py),
            (base[0] + nx * HEAD_W, base[1] + ny * HEAD_W),
            (base[0] - nx * HEAD_W, base[1] - ny * HEAD_W),
        ],
        fill=colour,
    )


def marker(d: ImageDraw.ImageDraw, centre, n: int, colour) -> None:
    """Draw the numbered disc that anchors an arrow."""
    cx, cy = pt(*centre)
    d.ellipse([cx - R, cy - R, cx + R, cy + R], fill=colour, outline=(10, 12, 16), width=4)
    d.text((cx, cy - 2), str(n), font=F_MARK, fill=(10, 12, 16), anchor="mm")


def wrap(text: str, f: ImageFont.FreeTypeFont, width: int) -> list[str]:
    lines, line = [], ""
    for word in text.split():
        probe = f"{line} {word}".strip()
        if f.getlength(probe) <= width and line:
            line = probe
        elif not line:
            line = word
        else:
            lines.append(line)
            line = word
    if line:
        lines.append(line)
    return lines


def build(fig: dict) -> None:
    src = Image.open(os.path.join(RAW, fig["src"])).convert("RGB")
    shot = src.crop((0, CROP_TOP, src.width, src.height))
    d = ImageDraw.Draw(shot)

    for i, c in enumerate(fig["callouts"], start=1):
        colour = GREEN if c.get("mine") else AMBER
        arrow(d, c["tail"], c["tip"], colour)
        marker(d, c["tail"], i, colour)

    # --- legend band -----------------------------------------------------
    pad, gap, indent = 64, 18, 96
    text_w = shot.width - pad * 2 - indent

    blocks = []
    for c in fig["callouts"]:
        head, _, rest = c["text"].partition(" — ")
        blocks.append((head, wrap(rest, F_LEGEND, text_w) if rest else []))

    lead_lines = wrap(fig["lead"], F_LEAD, shot.width - pad * 2)

    h = pad + 78 + len(lead_lines) * 60 + 34
    for head, rest in blocks:
        h += 60 + len(rest) * 56 + gap
    h += pad - gap

    band = Image.new("RGB", (shot.width, int(h)), BAND_BG)
    b = ImageDraw.Draw(band)
    b.rectangle([0, 0, band.width, 6], fill=AMBER)

    y = pad
    b.text((pad, y), fig["title"], font=F_TITLE, fill=(255, 255, 255))
    y += 78
    for line in lead_lines:
        b.text((pad, y), line, font=F_LEAD, fill=BAND_DIM)
        y += 60
    y += 34

    for i, ((head, rest), c) in enumerate(zip(blocks, fig["callouts"]), start=1):
        colour = GREEN if c.get("mine") else AMBER
        r = 26
        b.ellipse([pad, y + 8, pad + r * 2, y + 8 + r * 2], fill=colour)
        b.text((pad + r, y + 8 + r - 2), str(i), font=font("segoeuib.ttf", 32),
               fill=(10, 12, 16), anchor="mm")
        b.text((pad + indent, y), head, font=F_LEGEND_B, fill=colour)
        y += 60
        for line in rest:
            b.text((pad + indent, y), line, font=F_LEGEND, fill=BAND_FG)
            y += 56
        y += gap

    out = Image.new("RGB", (shot.width, shot.height + band.height), BAND_BG)
    out.paste(shot, (0, 0))
    out.paste(band, (0, shot.height))
    path = os.path.join(OUT, fig["out"])
    out.save(path, optimize=True)
    print(f"{path}  {out.width}x{out.height}")


# Each callout: tip is the pixel being pointed at, tail is where the numbered
# disc sits (chosen to land in empty UI space). mine=True colours it green.
FIGURES = [
    {
        "src": "s18.png",
        "out": "01-three-columns.png",
        "title": "1 · Three columns, and nowhere to chat",
        "lead": "Whetstone opens on loopback with a per-session token in the URL. "
                "What it does not open with is a prompt box — the model is reachable "
                "only through fixed affordances attached to text you chose.",
        "callouts": [
            {"tip": (285, 200), "tail": (600, 205),
             "text": "no question set — Say what you are trying to answer before you "
                     "read. Every lens and every objection is anchored to it, and they "
                     "are much sharper for it."},
            {"tip": (180, 300), "tail": (300, 500),
             "text": "SOURCES — Documents you paste. Importing one costs nothing and "
                     "reveals nothing: no request is made until you ask for one."},
            {"tip": (1020, 368), "tail": (1180, 470),
             "text": "The empty state is the thesis — \"You read the sections yourself. "
                     "The lens tells you where to look; the provocations argue with you. "
                     "Neither replaces the reading.\""},
            {"tip": (1545, 265), "tail": (1400, 480), "mine": True,
             "text": "YOUR ARGUMENT — Points, notes and citations, all typed by you. "
                     "Nothing in this program generates outline structure, so the shape "
                     "of the finished document is the shape you built."},
            {"tip": (1800, 220), "tail": (1300, 252),
             "text": "Export and reading room — .docx or PDF at any point, and hide "
                     "panels gives a long passage the whole window."},
        ],
    },
    {
        "src": "s17.png",
        "out": "02-your-question.png",
        "title": "2 · Your question is context, not a prompt",
        "lead": "This is the only free text that reaches the model, and it cannot ask "
                "for anything: it is pasted into fixed prompts as the thing you are "
                "trying to answer.",
        "callouts": [
            {"tip": (620, 320), "tail": (1120, 265), "mine": True,
             "text": "One question per line — Most important first. An investigation "
                     "with facets gets a line each, and each provocation is aimed at them."},
            {"tip": (895, 447), "tail": (1120, 500),
             "text": "One workspace per investigation — Genuinely separate work belongs "
                     "in its own file. Mixing unrelated questions blunts the objections "
                     "rather than doubling them."},
        ],
    },
    {
        "src": "s01.png",
        "out": "03-paste-a-document.png",
        "title": "3 · Paste a document. Nothing is sent anywhere.",
        "lead": "Splitting and cleaning happen locally, in Go, with no network call. "
                "The expensive, revealing part of most AI tools — ingesting your "
                "document — does not exist here.",
        "callouts": [
            {"tip": (332, 466), "tail": (660, 430),
             "text": "Markdown or plain text — A spec, a report, a transcript. Headings "
                     "become section boundaries; text with no headings is chunked on "
                     "paragraph breaks."},
            {"tip": (105, 750), "tail": (620, 800),
             "text": "Add document — Markdown is converted to prose so you read the "
                     "argument, not the formatting. Code fences keep their contents "
                     "byte-exact and snake_case identifiers are left alone."},
            {"tip": (1020, 368), "tail": (1200, 480),
             "text": "Still no model call — Nothing is summarised on import. The "
                     "document is left sitting there waiting for you to read it, which "
                     "is the whole design."},
        ],
    },
    {
        "src": "s02.png",
        "out": "04-sections-unread.png",
        "title": "4 · 34 sections, every one of them unread",
        "lead": "A 20,000-word document becomes a list of units of attention. What it "
                "does not become is a summary — the tool has formed no opinion about "
                "any of this yet.",
        "callouts": [
            {"tip": (33, 443), "tail": (268, 258),
             "text": "○ unread — Every section starts hollow and only you can fill it "
                     "in. Anything over ~700 words is split further; a long chapter "
                     "under one heading is not a useful unit of attention."},
            {"tip": (1840, 222), "tail": (1420, 252),
             "text": "2 of 34 sections read — The honest denominator of the engagement "
                     "report, and the tool says plainly that opening a section is not "
                     "the same as reading it."},
            {"tip": (775, 345), "tail": (1160, 350),
             "text": "No Technical reading of this section yet — The lens has not run. "
                     "Whetstone will not spend your tokens, or form your view, on its "
                     "own initiative."},
            {"tip": (935, 446), "tail": (1160, 452),
             "text": "Four actions, and that is all — Apply lens, Provoke, Edit text, "
                     "Delete section. There is no fifth button that writes the section "
                     "for you."},
        ],
    },
    {
        "src": "s03.png",
        "out": "05-a-lens-not-a-summary.png",
        "title": "5 · A lens, not a summary",
        "lead": "A summary answers \"what does this say?\" and replaces the reading. A "
                "lens answers \"what does this say that bears on my question?\" and "
                "directs it. The passage stays on screen, in full, underneath.",
        "callouts": [
            {"tip": (730, 404), "tail": (1150, 300),
             "text": "Where to look, and why — The system prompt forbids conclusions "
                     "and recommendations outright, and requires the model to say so "
                     "and score relevance low when a section does not bear on your "
                     "question."},
            {"tip": (700, 677), "tail": (975, 677),
             "text": "Four lenses — Claims & evidence, Technical, Risk, Method. The "
                     "same section reads differently through each, and switching is "
                     "cheaper than deciding what matters in advance."},
            {"tip": (935, 727), "tail": (1190, 733),
             "text": "Apply Technical lens — Lenses run cold, because orientation should be "
                     "stable: ask twice, get the same guidance."},
            {"tip": (655, 876), "tail": (1150, 900),
             "text": "The text is still here — You cannot get the finding without "
                     "reading the finding."},
        ],
    },
    {
        "src": "s07.png",
        "out": "06-it-argues-with-you.png",
        "title": "6 · It argues with you",
        "lead": "The critic's prompt forbids rewriting, improving, completing, praising, "
                "summarising and agreeing, and tells it to prefer the objection you are "
                "least likely to have already considered. A test asserts those clauses "
                "are still in the prompt, because prompts erode silently.",
        "callouts": [
            {"tip": (592, 304), "tail": (840, 300),
             "text": "Five kinds of pressure — counterargument, unstated assumption, "
                     "evidence gap, alternative reading, or a named fallacy. Each one "
                     "is labelled, so you can see what sort of trouble you are in."},
            {"tip": (1030, 392), "tail": (1210, 440),
             "text": "Every objection ends in a question — It hands the work back to "
                     "you instead of resolving itself. Provocations run hot, because "
                     "divergence is the product."},
            {"tip": (600, 433), "tail": (800, 437),
             "text": "Two verbs, and no third — Engage (what did you change?) or "
                     "Dismiss (why does it not apply?). There is no \"accept\" verb "
                     "anywhere in the codebase."},
            {"tip": (592, 861), "tail": (850, 866),
             "text": "OPEN — It stays open. Not a notification you can swipe away: a "
                     "piece of unfinished thinking, and it will follow you into the "
                     "export."},
            {"tip": (1620, 220), "tail": (1330, 252),
             "text": "4 objections unanswered — Kept in front of you at all times, "
                     "next to the share of the words that are yours."},
        ],
    },
    {
        "src": "s04.png",
        "out": "07-select-a-line-make-it-a-point.png",
        "title": "7 · Select a line, make it your point",
        "lead": "The friction people skip is citing their evidence. So the citation is "
                "a by-product of making the claim, not a second job you do later.",
        "callouts": [
            {"tip": (1285, 627), "tail": (1255, 478),
             "text": "Your selection — Highlight the sentence that actually matters. "
                     "The bar appears only while text is selected."},
            {"tip": (960, 950), "tail": (1330, 770), "mine": True,
             "text": "+ new point — Your claim, with that exact sentence attached as "
                     "its citation, in one action. cite into attaches it to the point "
                     "you are already on; quote in notes of drops it into your notes "
                     "and puts the cursor on the line below, so you answer it now."},
            {"tip": (1410, 955), "tail": (1430, 830),
             "text": "say it differently… — Seven dimensions — plainer, tighter, more "
                     "formal, more rigorous, fuller, more practical, more "
                     "inspirational — offered on text you wrote, never on the source."},
            {"tip": (1620, 278), "tail": (1700, 500), "mine": True,
             "text": "Still empty — Nothing has appeared in your argument by itself, "
                     "and nothing will."},
        ],
    },
    {
        "src": "s05.png",
        "out": "08-the-argument-is-yours.png",
        "title": "8 · The argument is structurally yours",
        "lead": "Generated prose is downstream of human judgement rather than the other "
                "way round — the difference between \"AI wrote this and I edited it\" "
                "and \"I wrote this and AI typed it\".",
        "callouts": [
            {"tip": (1700, 447), "tail": (1290, 455), "mine": True,
             "text": "YOUR NOTES — Your reasoning, autosaved as you type. This is the "
                     "material every draft is built from."},
            {"tip": (1830, 583), "tail": (1330, 600), "mine": True,
             "text": "CITED FROM YOUR SOURCES — The exact excerpt you selected, tagged "
                     "with the section it came from. Edit the source later and a "
                     "citation that no longer matches is flagged stale rather than "
                     "silently breaking."},
            {"tip": (1895, 318), "tail": (1400, 300), "mine": True,
             "text": "1 cited — The point carries its evidence with it, everywhere it "
                     "goes, including into the export."},
            {"tip": (1570, 950), "tail": (1200, 995),
             "text": "Draft · Compose… · Review my writing — Draft builds a paragraph "
                     "from your notes and citations. Compose… takes an instruction "
                     "about arrangement (\"lead with the cost objection\"). With "
                     "neither notes nor citations, both refuse to run: an instruction "
                     "is not source material. Review my writing turns the critic on "
                     "your own argument."},
            {"tip": (1815, 222), "tail": (1420, 252), "mine": True,
             "text": "100% of the words are yours — Nobody decides to stop thinking; "
                     "they accept one draft and then another. An edited draft splits "
                     "between the two counts rather than being claimed by either side."},
        ],
    },
    {
        "src": "s11.png",
        "out": "09-dismissing-costs-a-sentence.png",
        "title": "9 · Resolving an objection costs you a sentence",
        "lead": "Dismiss(\"\") returns an error, and there is a test for it. Writing the "
                "sentence is the thinking the tool exists to cause — and if you can "
                "write it confidently, the process worked.",
        "callouts": [
            {"tip": (720, 693), "tail": (1030, 690),
             "text": "WHAT DID YOU CHANGE OR DECIDE? — Engage asks what moved. Dismiss "
                     "asks why the objection does not apply. Neither accepts blank input."},
            {"tip": (950, 790), "tail": (1230, 866), "mine": True,
             "text": "Your reason, in your words — A reasoned dismissal is a success, "
                     "not a rejection. That is why there is no \"accept\" anywhere: "
                     "clearing an objection without thinking is the one path the design "
                     "closes off."},
            {"tip": (470, 862), "tail": (700, 890),
             "text": "Record — Stores the sentence against the objection, where it "
                     "stays: visible in the app, and carried into every export."},
        ],
    },
    {
        "src": "s12.png",
        "out": "10-resolved-not-deleted.png",
        "title": "10 · Resolved, and on the record",
        "lead": "The count moves only when you write something. That is the whole "
                "mechanism: a number you cannot improve without doing the work it "
                "measures.",
        "callouts": [
            {"tip": (640, 876), "tail": (880, 880),
             "text": "ENGAGED — The objection is not deleted. It stays, relabelled, "
                     "with your answer beneath it, so the reasoning is auditable later."},
            {"tip": (1085, 996), "tail": (1235, 1008), "mine": True,
             "text": "you: — Your sentence, attributed to you and kept verbatim beside "
                     "the objection it answers."},
            {"tip": (1600, 200), "tail": (1330, 252),
             "text": "3 objections unanswered — Down from four. No button, no bulk "
                     "action and no timeout will move this number for you."},
        ],
    },
    {
        "src": "s14.png",
        "out": "11-review-my-writing.png",
        "title": "11 · Then it argues with you about your own argument",
        "lead": "The critic can object to your reasoning. What it cannot do is "
                "restructure your outline, rewrite your prose, or add a claim you did "
                "not make — those paths do not exist, so no amount of asking opens them.",
        "callouts": [
            {"tip": (1777, 589), "tail": (1300, 622),
             "text": "Review my writing — The same critic, the same five kinds of "
                     "objection, pointed at your point instead of the source passage."},
            {"tip": (1750, 924), "tail": (1300, 941),
             "text": "An objection to you — \"You assume that the collision of actions "
                     "will always lead to an incorrect execution.\" It is arguing with "
                     "your inference, not with the document."},
            {"tip": (1600, 962), "tail": (1240, 1000),
             "text": "Engage · Dismiss — Your own claim has to survive a reason too. "
                     "The constraint does not soften just because the target is you."},
        ],
    },
]


if __name__ == "__main__":
    for figure in FIGURES:
        build(figure)
