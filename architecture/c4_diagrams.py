"""Generate lazyreckon's C4 diagrams into assets/.

Run:  python3 architecture/c4_diagrams.py

  c4-context.svg     Level 1: lazyreckon and every system it talks to
  c4-components.svg  Level 3: what the one binary is made of

Every box is a package that exists on this branch (cmd/lazyreckon, internal/*),
every arrow an import or a call the code makes. The gateway services named in
architecture/README.md are the ones the views actually call through reckon-go.
When the code changes, change this file and re-run it: a diagram that still names
a component the binary lost is drift.

Colours are plain classes with a prefers-color-scheme: dark block, no CSS custom
properties (rsvg-convert renders those black). Render and look after a change:
rsvg-convert -w 1400 assets/c4-context.svg -o /tmp/c4.png
"""
import os
from xml.sax.saxutils import escape

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "assets")

SANS = '"Atkinson Hyperlegible", "Segoe UI", system-ui, -apple-system, Helvetica, Arial, sans-serif'
MONO = '"JetBrains Mono", ui-monospace, Menlo, Consolas, monospace'

KINDS = {
    "b-sys": ("#F5E9CC", "#A56F0A", "#3A2E13", "#E3AA3C"),
    "b-per": ("#E3EBF2", "#3B5B7A", "#1D2A36", "#8EB4D8"),
    "b-mac": ("#E6E4EE", "#5E5A78", "#26243A", "#A9A4C8"),
    "b-ext": ("#ECEFED", "#8A9791", "#1C2421", "#7D8B85"),
    "b-cmp": ("#FFFFFF", "#8A9791", "#171F1C", "#7D8B85"),
}

STYLE = f"""<style>
.t-name {{ font-family: {SANS}; font-size: 15px; font-weight: 700; }}
.t-desc {{ font-family: {SANS}; font-size: 12.5px; }}
.t-rel {{ font-family: {SANS}; font-size: 11.5px; paint-order: stroke; stroke-width: 4px; stroke-linejoin: round; }}
.t-code {{ font-family: {MONO}; font-size: 13px; font-weight: 600; }}
.t-type {{ font-family: {MONO}; font-size: 11.5px; }}
.t-bound {{ font-family: {MONO}; font-size: 12px; font-weight: 600; }}
.t-leg {{ font-family: {SANS}; font-size: 12px; }}
.edge {{ fill: none; stroke-width: 1.25; }}
.edge-later {{ fill: none; stroke-width: 1.25; stroke-dasharray: 5 4; }}
.bg {{ fill: #FFFFFF; }}
""" + "".join(f".{k} {{ fill: {v[0]}; stroke: {v[1]}; stroke-width: {2 if k == 'b-sys' else 1.25}; }}\n" for k, v in KINDS.items()) + """\
.b-later { fill: none; stroke: #8A9791; stroke-width: 1.25; stroke-dasharray: 5 4; }
.bound { fill: none; stroke: #B3BDB8; stroke-width: 1; }
.head { fill: #3B5B7A; }
.t-name, .t-code { fill: #16201D; }
.t-type, .t-bound, .t-leg { fill: #66736E; }
.t-desc { fill: #34413C; }
.t-rel { fill: #66736E; stroke: #FFFFFF; }
.edge, .edge-later { stroke: #8A9791; }
.arrowhead { fill: #8A9791; }
@media (prefers-color-scheme: dark) {
.bg { fill: #171F1C; }
""" + "".join(f".{k} {{ fill: {v[2]}; stroke: {v[3]}; }}\n" for k, v in KINDS.items()) + """\
.b-later { stroke: #7D8B85; }
.bound { stroke: #4A5752; }
.head { fill: #8EB4D8; }
.t-name, .t-code { fill: #E6ECE9; }
.t-type, .t-bound, .t-leg { fill: #93A09A; }
.t-desc { fill: #C3CDC8; }
.t-rel { fill: #93A09A; stroke: #171F1C; }
.edge, .edge-later { stroke: #7D8B85; }
.arrowhead { fill: #7D8B85; }
}
</style>"""

DEFS = ('<defs><marker id="a" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" '
        'orient="auto-start-reverse"><path class="arrowhead" d="M0 0L10 5L0 10z"/></marker></defs>')

LEGEND = [("b-sys", "In scope"), ("b-per", "Person"), ("b-mac", "Platform it runs on"),
          ("b-ext", "External system"), ("b-cmp", "Component")]
BOUNDARY = ("bound", "Boundary")


def height_for(desc):
    return 74 + 16 * len(desc)


class Diagram:
    def __init__(self, name, title, w, h):
        self.name, self.title, self.w, self.h, self.parts = name, title, w, h, []

    def text(self, cls, x, y, s, anchor=None, rotate=None):
        a = f' text-anchor="{anchor}"' if anchor else ""
        r = f' transform="rotate({rotate} {x} {y})"' if rotate is not None else ""
        self.parts.append(f'<text class="{cls}" x="{x}" y="{y}"{a}{r}>{escape(s)}</text>')

    def box(self, cls, x, y, w, name, kind, desc, person=False, code=False):
        h = height_for(desc)
        self.parts.append(f'<rect class="{cls}" x="{x}" y="{y}" width="{w}" height="{h}" rx="10"/>')
        tx = x + 16
        if person:
            self.parts.append(f'<circle class="head" cx="{x + 26}" cy="{y + 24}" r="9"/>')
            tx = x + 44
        self.text("t-code" if code else "t-name", tx, y + 28, name)
        self.text("t-type", tx, y + 46, kind)
        for i, line in enumerate(desc):
            self.text("t-desc", x + 16, y + 70 + 16 * i, line)
        return h

    def bound(self, x, y, w, h, label):
        self.parts.append(f'<rect class="bound" x="{x}" y="{y}" width="{w}" height="{h}" rx="12"/>')
        self.text("t-bound", x + 16, y + 22, label)

    def edge(self, d, later=False):
        self.parts.append(f'<path class="{"edge-later" if later else "edge"}" d="{d}" marker-end="url(#a)"/>')

    def legend(self, y, boundary=True):
        x = 24
        for cls, label in LEGEND + ([BOUNDARY] if boundary else []):
            self.parts.append(f'<rect class="{cls}" x="{x}" y="{y}" width="22" height="14" rx="3"/>')
            self.text("t-leg", x + 30, y + 12, label)
            x += 40 + 6.9 * len(label)

    def write(self):
        svg = [f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {self.w} {self.h}" role="img" '
               f'aria-label="{escape(self.title)}">', f"<title>{escape(self.title)}</title>", STYLE, DEFS,
               f'<rect class="bg" x="0" y="0" width="{self.w}" height="{self.h}"/>'] + self.parts + ["</svg>", ""]
        with open(os.path.join(OUT, self.name), "w") as f:
            f.write("\n".join(svg))


def context():
    d = Diagram("c4-context.svg", "lazyreckon system context", 1000, 640)
    d.box("b-per", 375, 30, 250, "Operator at a terminal", "[Person]",
          ["Inspects stores, cluster health,", "streams, subscriptions and", "snapshots, the way lazygit browses git"], person=True)
    d.box("b-ext", 30, 210, 250, "reckon-gateway", "[External: gRPC, usually 50051]",
          ["The only peer it has. Stores,", "Stream, Subscription, Snapshot",
           "and Health services over a", "connection it dials with TLS",
           "verified against system roots"])
    d.box("b-sys", 375, 210, 250, "lazyreckon", "[In scope: a Go TUI binary]",
          ["A terminal UI over one gateway", "endpoint at a time: a four-pane",
           "cluster dashboard, then drill", "paths into streams, subscriptions",
           "and snapshots, with a live health"])
    d.box("b-ext", 720, 210, 250, "Profiles and editor", "[External: local files]",
          ["profiles.toml under the XDG config", "directory holds the saved endpoints;",
           "the $EDITOR handoff writes a JSON", "copy under the cache directory"])
    d.box("b-ext", 375, 430, 250, "Terminal", "[External: a TTY]",
          ["Keys, a resizable frame, and the", "alternate screen the TUI draws on"])
    d.edge("M500 157V210"); d.text("t-rel", 508, 188, "browses, drills")
    d.edge("M375 285H280"); d.text("t-rel", 327, 268, "gRPC", anchor="middle"); d.text("t-rel", 327, 305, "over TLS", anchor="middle")
    d.edge("M625 285H720"); d.text("t-rel", 672, 268, "reads, writes", anchor="middle")
    d.edge("M500 364V430"); d.text("t-rel", 508, 400, "draws, reads keys")
    d.text("t-desc", 30, 560, "It never speaks to a store directly: every fact on screen arrives through the gateway's own services,")
    d.text("t-desc", 30, 576, "so an endpoint that is down is the whole failure surface. TLS is not optional here: no flag turns it off.")
    d.legend(610, boundary=False)
    d.write()


def components():
    d = Diagram("c4-components.svg", "lazyreckon components", 1000, 960)
    d.bound(16, 40, 968, 690, "lazyreckon binary [Container: one Go binary, Bubble Tea and lipgloss]")
    d.box("b-cmp", 32, 70, 624, "cmd/lazyreckon: the root model", "[tea.Model, key routing, mode switching]",
          ["Resolves the endpoint from --endpoint, --profile or the picker, dials with a 5 s timeout,",
           "then runs the Bubble Tea program. Holds the four modes, the filter and goto command bar,",
           "the breadcrumb, and the 5 s health tick."], code=True)
    d.box("b-cmp", 664, 70, 304, "internal/ui, internal/theme", "[chrome]",
          ["Header, breadcrumb, status bar and", "help overlay; the palette and the",
           "◉ glyph every view shares."], code=True)
    d.box("b-cmp", 32, 228, 296, "internal/modes: stores", "[StoresView, four columns]",
          ["The cluster dashboard: nodes and", "node detail, stores and store info,",
           "fed by internal/cluster and", "refreshed on the health tick."], code=True)
    d.box("b-cmp", 348, 228, 296, "internal/modes: drill views", "[Streams, Subscriptions, Snapshots]",
          ["Streams to events to detail,", "subscriptions with live lag, and",
           "snapshot streams to versions to", "data, each its own column set."], code=True)
    d.box("b-cmp", 664, 228, 304, "internal/ranger", "[the layout engine]",
          ["Column and Crumber interfaces with", "two orchestrators: Ranger for the",
           "fixed grid and Drill for the", "breadcrumb hierarchy."], code=True)
    d.box("b-cmp", 32, 390, 296, "internal/cluster", "[Topology, StoreHealth]",
          ["Applies WatchStores events to a", "(store, node) map, keeps each",
           "store's health, and parses the", "leader, term and failed-node text."], code=True)
    d.box("b-cmp", 348, 390, 296, "internal/splash, internal/profiles", "[the picker and its file]",
          ["A first Bubble Tea program that", "lists, adds, renames, deletes and",
           "tests profiles; profiles.toml is", "TOML and holds no secrets."], code=True)
    d.box("b-cmp", 664, 390, 304, "internal/editor", "[the $EDITOR handoff]",
          ["Serializes the selected record to", "a JSON cache file and runs the",
           "editor through tea.ExecProcess,", "then reports DoneMsg."], code=True)
    d.box("b-mac", 32, 556, 624, "reckon-go v0.10.0", "[Platform: the SDK this binary is built on]",
          ["reckon.Client with Stores, Streams, Subscriptions and Snapshots sub-clients over grpc-go,",
           "plus the generated reckon.gateway.v1 stubs the health probes call directly."], code=True)
    d.box("b-ext", 32, 760, 304, "reckon-gateway", "[External endpoint]",
          ["One host:port, TLS by default", "with no flag to disable it"], None)
    d.edge("M240 166V228"); d.text("t-rel", 248, 200, "builds every view")
    d.edge("M816 166V228"); d.text("t-rel", 824, 200, "renders through")
    d.edge("M180 356V390"); d.text("t-rel", 188, 378, "reads")
    d.edge("M498 356V390"); d.text("t-rel", 506, 378, "reads, writes")
    d.edge("M816 356V390"); d.text("t-rel", 824, 378, "hands off")
    d.edge("M180 528V556"); d.text("t-rel", 188, 546, "calls")
    d.edge("M498 528V556"); d.text("t-rel", 506, 546, "calls")
    d.edge("M180 694V760"); d.text("t-rel", 188, 730, "grpc-go over TLS")
    d.legend(890)
    d.write()


if __name__ == "__main__":
    for make in (context, components):
        make()
