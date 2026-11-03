#!/usr/bin/env python3
"""Build a realistic, anonymous demo workspace for glint's README previews."""
import os, random, subprocess, shutil, time, math, struct, zlib
from datetime import datetime, timedelta

random.seed(7)
ROOT = "/var/tmp/glint-demo/home"
P = os.path.join(ROOT, "projects")
NOW = datetime.now()
shutil.rmtree(ROOT, ignore_errors=True)
os.makedirs(P)


def w(path, text, ago=None):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(text.lstrip("\n"))
    if ago is not None:
        t = (NOW - ago).timestamp()
        os.utime(path, (t, t))


def blob(path, size, ago=None):
    """Allocate real disk blocks fast (shows up in du and the disk map)."""
    os.makedirs(os.path.dirname(path), exist_ok=True)
    subprocess.run(["fallocate", "-l", str(size), path], check=True)
    if ago is not None:
        t = (NOW - ago).timestamp()
        os.utime(path, (t, t))


def png(path, wd, ht, fn):
    rows = b""
    for y in range(ht):
        row = b"\x00"
        for x in range(wd):
            r, g, b = fn(x, y)
            row += bytes((max(0, min(255, int(r))), max(0, min(255, int(g))), max(0, min(255, int(b)))))
        rows += row
    def chunk(t, d):
        c = struct.pack(">I", len(d)) + t + d
        return c + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)
    data = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", wd, ht, 8, 2, 0, 0, 0))
    data += chunk(b"IDAT", zlib.compress(rows, 9)) + chunk(b"IEND", b"")
    os.makedirs(os.path.dirname(path), exist_ok=True)
    open(path, "wb").write(data)


def git(repo, *args, date=None):
    env = dict(os.environ, GIT_AUTHOR_NAME="Ada Rivers", GIT_AUTHOR_EMAIL="ada@example.com",
               GIT_COMMITTER_NAME="Ada Rivers", GIT_COMMITTER_EMAIL="ada@example.com")
    if date:
        env["GIT_AUTHOR_DATE"] = env["GIT_COMMITTER_DATE"] = date.strftime("%Y-%m-%dT%H:%M:%S")
    subprocess.run(["git", "-C", repo, *args], env=env, check=True, capture_output=True)


def history(repo, subjects, weeks_back, last_ago, burst=None):
    """Commit `subjects` spread over the last weeks (busier recently)."""
    git(repo, "init", "-q", "-b", "main")
    git(repo, "add", "-A")
    n = len(subjects)
    start = NOW - timedelta(weeks=weeks_back)
    end = NOW - last_ago
    dates = sorted(start + (end - start) * (random.random() ** (0.55 if burst else 1)) for _ in range(n - 1)) + [end]
    marker = os.path.join(repo, ".git", "glint-demo")
    for i, (s, d) in enumerate(zip(subjects, dates)):
        if i:
            with open(marker, "a") as f:
                f.write(s)
        git(repo, "commit", "-q", "--allow-empty", "-m", s, date=d)


def touch_tree(repo, ago):
    t = (NOW - ago).timestamp()
    for dp, dn, fn in os.walk(repo):
        if "/.git" in dp:
            continue
        os.utime(dp, (t, t))
        for f in fn:
            try:
                os.utime(os.path.join(dp, f), (t, t), follow_symlinks=False)
            except OSError:
                pass


# ------------------------------------------------------------------ code generators

def rust_fns(names, kind="pub fn"):
    out = []
    for n in names:
        out.append(f"""
/// {n.replace('_', ' ').capitalize()}.
{kind} {n}(ctx: &mut Context, input: &[f32]) -> Result<f32, Error> {{
    let mut acc = 0.0;
    for (i, v) in input.iter().enumerate() {{
        acc += v * ctx.weights[i % ctx.weights.len()];
    }}
    if acc.is_nan() {{
        return Err(Error::Diverged("{n}"));
    }}
    Ok(acc / input.len().max(1) as f32)
}}
""")
    return "".join(out)


def go_fns(names):
    return "".join(f"""
// {n} handles the {n.lower()} endpoint.
func (s *Server) {n}(w http.ResponseWriter, r *http.Request) {{
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	items, err := s.store.List(ctx, r.URL.Query().Get("q"))
	if err != nil {{
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}}
	writeJSON(w, items)
}}
""" for n in names)


def py_fns(names):
    return "".join(f'''

def {n}(doc: Document, *, strict: bool = False) -> list[str]:
    """{n.replace('_', ' ').capitalize()}."""
    out = []
    for block in doc.blocks:
        if strict and not block.text.strip():
            raise ValueError(f"empty block in {{doc.path}}")
        out.append(block.text)
    return out
''' for n in names)


# ------------------------------------------------------------------ aurora: Rust workspace (the hero)

A = os.path.join(P, "aurora")
w(f"{A}/Cargo.toml", """
[workspace]
members = ["crates/*"]
resolver = "2"

[workspace.package]
version = "0.8.2"
edition = "2021"
license = "MIT"
""")
w(f"{A}/README.md", "# Aurora\n\nA tiny path tracer with a GPU denoiser.\n\n" + "\n".join(f"- feature {i}" for i in range(30)) + "\n")
w(f"{A}/LICENSE", "MIT License\n\nCopyright (c) 2026 Ada Rivers\n" + "\nPermission is hereby granted...\n" * 6)
w(f"{A}/.gitignore", "target/\n*.profraw\n")
w(f"{A}/rust-toolchain.toml", '[toolchain]\nchannel = "1.90"\n')
w(f"{A}/Cargo.lock", "# generated\nversion = 4\n" + "".join(f'\n[[package]]\nname = "dep{i}"\nversion = "1.{i}.0"\n' for i in range(380)))
for crate, files in {
    "aurora-core": {"lib.rs": ["trace", "shade", "bounce", "sample_light"], "scene.rs": ["load_scene", "build_bvh", "intersect"],
                    "math.rs": ["dot3", "cross3", "reflect", "refract", "fresnel"], "camera.rs": ["look_at", "ray_for_pixel"]},
    "aurora-render": {"lib.rs": ["present", "resize"], "gpu.rs": ["create_device", "upload_scene", "dispatch", "readback"],
                      "denoise.rs": ["bilateral", "temporal_reproject"]},
    "aurora-cli": {"main.rs": ["parse_args", "run", "write_png"]},
}.items():
    w(f"{A}/crates/{crate}/Cargo.toml", f'[package]\nname = "{crate}"\nversion.workspace = true\nedition.workspace = true\n')
    for f, fns in files.items():
        head = "use crate::{Context, Error};\n\n" if f != "main.rs" else "use aurora_core::{Context, Error};\n\n"
        w(f"{A}/crates/{crate}/src/{f}", head + "pub struct Context {\n    pub weights: Vec<f32>,\n}\n" + rust_fns(fns) * 2)
w(f"{A}/crates/aurora-render/src/shaders/denoise.wgsl", "@compute @workgroup_size(8, 8)\nfn main(@builtin(global_invocation_id) id: vec3<u32>) {\n" + "    // filter tap\n" * 40 + "}\n")
w(f"{A}/docs/architecture.md", "# Architecture\n\n## Core\n\n## Render\n\n## CLI\n" + "\nlorem ipsum\n" * 40)
png(f"{A}/assets/logo.png", 96, 96, lambda x, y: (120 + x, 80 + y, 230 - x))
for i, name in enumerate(["checker", "marble", "sunset"]):
    png(f"{A}/assets/textures/{name}.png", 64, 64,
        lambda x, y, i=i: ((x // 8 + y // 8) % 2 * 200 + 30, 100 + 40 * i, 180 - 50 * i) if i == 0 else
        (128 + 120 * math.sin(x / 7 + y / 11), 90 + 60 * i, 200 - y * 2))
history(A, ["Initial path tracer", "Add BVH", "Camera depth of field", "Fresnel for glass", "GPU device setup",
            "Upload scene buffers", "Bilateral denoiser", "Temporal reprojection", "CLI: write PNG", "Faster BVH build",
            "Fix NaN in shade", "Importance sampling", "Docs: architecture", "Bump wgpu", "Denoise in compute shader",
            "Tile scheduler", "Progressive preview", "HDR output", "Fix camera aspect", "Release 0.8.2",
            "Profile hot loop", "SIMD intersect", "Clamp fireflies", "GPU: async readback", "Shader hot reload"]
        , weeks_back=11, last_ago=timedelta(hours=3), burst=True)
touch_tree(A, timedelta(hours=3))
with open(f"{A}/crates/aurora-render/src/gpu.rs", "a") as f:
    f.write("\n// TODO: double-buffer the readback\n")
w(f"{A}/crates/aurora-render/src/upscale.rs", "use crate::{Context, Error};\n" + rust_fns(["upscale", "sharpen"]), ago=timedelta(minutes=12))
blob(f"{A}/target/debug/deps/libaurora_core.rlib", 380 << 20, ago=timedelta(hours=3))
blob(f"{A}/target/debug/incremental/aurora-1x2y/dep-graph.bin", 260 << 20, ago=timedelta(hours=3))
blob(f"{A}/target/release/aurora", 41 << 20, ago=timedelta(days=4))
blob(f"{A}/target/release/deps/libwgpu.rlib", 220 << 20, ago=timedelta(days=4))

# ------------------------------------------------------------------ nebula: Next.js site
N = os.path.join(P, "nebula")
w(f"{N}/package.json", """
{
  "name": "nebula",
  "private": true,
  "scripts": { "dev": "next dev", "build": "next build" },
  "dependencies": { "next": "15.2.0", "react": "19.0.0", "react-dom": "19.0.0" },
  "devDependencies": { "typescript": "5.7.2", "tailwindcss": "4.0.0" }
}
""")
w(f"{N}/tsconfig.json", '{ "compilerOptions": { "strict": true, "jsx": "preserve" } }\n')
w(f"{N}/next.config.mjs", "export default { reactStrictMode: true };\n")
w(f"{N}/.gitignore", "node_modules/\n.next/\n")
for f in ["page", "layout", "blog/page", "blog/[slug]/page", "about/page"]:
    w(f"{N}/app/{f}.tsx", "export default function Page() {\n  return (\n    <main className=\"mx-auto max-w-3xl\">\n" + "      <p>content</p>\n" * 25 + "    </main>\n  );\n}\n")
for c in ["Header", "Footer", "PostCard", "ThemeToggle", "Search"]:
    w(f"{N}/components/{c}.tsx", f"export function {c}() {{\n  return <div className=\"{c.lower()}\" />;\n}}\n" + "\n// styles\n" * 12)
w(f"{N}/lib/api.ts", "export async function getPosts() {\n  const res = await fetch('/api/posts');\n  return res.json();\n}\n" * 6)
png(f"{N}/public/og.png", 120, 63, lambda x, y: (20 + x, 30 + y * 2, 90 + x // 2))
history(N, ["Bootstrap Next.js", "Blog index", "Post pages", "Dark mode", "Search", "RSS feed", "OG images", "Upgrade to Next 15"],
        weeks_back=9, last_ago=timedelta(days=2))
touch_tree(N, timedelta(days=2))
blob(f"{N}/node_modules/next/dist/compiled/next-server.js", 96 << 20, ago=timedelta(days=2))
blob(f"{N}/node_modules/@swc/core-linux-x64-gnu/swc.node", 54 << 20, ago=timedelta(days=2))
blob(f"{N}/.next/cache/webpack/client.pack", 38 << 20, ago=timedelta(days=2))

# ------------------------------------------------------------------ pixel-forge: Godot game
G = os.path.join(P, "pixel-forge")
w(f"{G}/project.godot", '[application]\n\nconfig/name="Pixel Forge"\nrun/main_scene="res://scenes/main.tscn"\n')
for s in ["main", "player", "forge", "inventory", "title"]:
    w(f"{G}/scenes/{s}.tscn", f'[gd_scene format=3]\n\n[node name="{s.title()}" type="Node2D"]\n' + '[node name="Sprite" type="Sprite2D"]\n' * 8)
for s, fns in {"player": ["_ready", "_physics_process", "jump", "take_damage"], "forge": ["_ready", "smelt", "hammer", "quench"],
               "inventory": ["add_item", "remove_item", "has"], "game_state": ["save", "load"]}.items():
    w(f"{G}/scripts/{s}.gd", "extends Node2D\n\nsignal changed\n\n" + "".join(f"func {f}(delta := 0.0) -> void:\n\tpass\n\n" for f in fns) * 3)
palette = [(244, 162, 97), (231, 111, 81), (42, 157, 143), (233, 196, 106), (38, 70, 83)]
for i, name in enumerate(["anvil", "hammer", "ingot", "slime", "torch", "chest"]):
    col = palette[i % len(palette)]
    png(f"{G}/assets/sprites/{name}.png", 32, 32,
        lambda x, y, c=col: c if (x - 16) ** 2 + (y - 16) ** 2 < 140 + 40 * math.sin(x) else (20, 20, 30))
for s in ["clang", "hiss", "coin", "jump"]:
    blob(f"{G}/assets/sfx/{s}.ogg", random.randint(40, 160) << 10)
w(f"{G}/.gitignore", ".godot/\nexport/\n")
history(G, ["Project setup", "Player movement", "Forge minigame", "Inventory", "Pixel art pass", "Sound effects", "Save games", "Title screen"],
        weeks_back=7, last_ago=timedelta(days=1), burst=True)
touch_tree(G, timedelta(days=1))
with open(f"{G}/scripts/forge.gd", "a") as f:
    f.write("\nfunc temper() -> void:\n\tpass\n")
w(f"{G}/scripts/combo.gd", "extends Node\n\nfunc chain(n: int) -> int:\n\treturn n * 2\n", ago=timedelta(minutes=40))
blob(f"{G}/.godot/imported/sprites.ctex", 64 << 20, ago=timedelta(days=1))
blob(f"{G}/.godot/shader_cache/cache.bin", 48 << 20, ago=timedelta(days=1))

# ------------------------------------------------------------------ atlas: Go API
T = os.path.join(P, "atlas")
w(f"{T}/go.mod", "module github.com/acme/atlas\n\ngo 1.24\n")
w(f"{T}/cmd/atlas/main.go", "package main\n\nimport \"log\"\n\nfunc main() {\n\tlog.Fatal(run())\n}\n")
w(f"{T}/internal/server/server.go", "package server\n\nimport (\n\t\"context\"\n\t\"net/http\"\n\t\"time\"\n)\n" + go_fns(["ListPlaces", "GetPlace", "CreatePlace", "Search", "Health"]))
w(f"{T}/internal/store/store.go", "package store\n\n// Store keeps places in Postgres.\ntype Store struct{}\n" + go_fns(["List", "Get", "Put"]).replace("(s *Server)", "(s *Store)"))
w(f"{T}/internal/geo/geo.go", "package geo\n\n// Haversine distance in km.\nfunc Distance(a, b Point) float64 {\n\treturn 0\n}\n")
w(f"{T}/Makefile", "build:\n\tgo build ./cmd/atlas\n\ntest:\n\tgo test ./...\n")
w(f"{T}/Dockerfile", "FROM golang:1.24 AS build\nWORKDIR /src\nCOPY . .\nRUN go build -o /atlas ./cmd/atlas\n")
history(T, ["Skeleton", "Places CRUD", "Postgres store", "Geo search", "Dockerfile", "Rate limiting", "Health endpoint"],
        weeks_back=5, last_ago=timedelta(hours=5))
touch_tree(T, timedelta(hours=5))

# ------------------------------------------------------------------ quill: Python CLI, no commits yet
Q = os.path.join(P, "quill")
w(f"{Q}/pyproject.toml", '[project]\nname = "quill"\nversion = "0.1.0"\nrequires-python = ">=3.11"\n')
w(f"{Q}/src/quill/__init__.py", '__version__ = "0.1.0"\n')
w(f"{Q}/src/quill/cli.py", "import argparse\n\nfrom .render import Document\n" + py_fns(["main", "parse"]))
w(f"{Q}/src/quill/render.py", "from dataclasses import dataclass\n\n@dataclass\nclass Document:\n    path: str\n    blocks: list\n" + py_fns(["to_html", "to_markdown", "word_count"]))
w(f"{Q}/tests/test_render.py", "def test_word_count():\n    assert True\n" * 8)
w(f"{Q}/.gitignore", ".venv/\n__pycache__/\n")
subprocess.run(["git", "-C", Q, "init", "-q", "-b", "main"], check=True)
touch_tree(Q, timedelta(days=6))
blob(f"{Q}/.venv/lib/python3.12/site-packages/numpy/core/_multiarray.so", 34 << 20, ago=timedelta(days=6))

# ------------------------------------------------------------------ dotfiles
D = os.path.join(P, "dotfiles")
w(f"{D}/fish/config.fish", "set -gx EDITOR nvim\nalias l 'glint -l'\n")
w(f"{D}/nvim/init.lua", "vim.opt.number = true\n" * 30)
w(f"{D}/install.sh", "#!/bin/sh\nset -e\nln -sf \"$PWD/fish\" ~/.config/fish\n")
os.chmod(f"{D}/install.sh", 0o755)
w(f"{D}/README.md", "# dotfiles\n")
history(D, ["Initial dotfiles", "fish: abbreviations", "nvim: lsp", "install script"], weeks_back=12, last_ago=timedelta(days=24))
touch_tree(D, timedelta(days=24))

# ------------------------------------------------------------------ sketches: images, no git
S = os.path.join(P, "sketches")
arts = {
    "aurora-borealis": lambda x, y: (30 + 40 * math.sin(x / 9), 120 + 100 * math.sin(x / 13 + y / 21), 140 + y),
    "dunes": lambda x, y: (230 - y, 160 - y // 2 + 20 * math.sin(x / 10 + y / 6), 90),
    "ocean": lambda x, y: (20, 80 + y, 150 + 80 * math.sin(x / 15 + y / 9)),
    "forest": lambda x, y: (30 + y // 3, 110 + 60 * math.sin(x / 5), 60),
    "neon-city": lambda x, y: (200 * ((x // 12) % 2), 40 + y, 220 - (y * 2) % 200),
    "sunrise": lambda x, y: (250, 110 + y, 80 + (x * y) % 90),
}
for i, (name, fn) in enumerate(arts.items()):
    png(f"{S}/{name}.png", 160, 100, fn)
    t = (NOW - timedelta(days=30 + i * 9)).timestamp()
    os.utime(f"{S}/{name}.png", (t, t))
w(f"{S}/ideas.md", "# ideas\n\n- color study\n- night skies\n", ago=timedelta(days=12))

# ------------------------------------------------------------------ archive: old stuff
R = os.path.join(P, "archive-2024")
for name in ["website-v1.tar.gz", "game-jam.zip", "thesis-drafts.tar.gz"]:
    blob(f"{R}/{name}", random.randint(30, 220) << 20, ago=timedelta(days=400))
os.makedirs(f"{P}/scratch")  # empty
t = (NOW - timedelta(days=18)).timestamp()
os.utime(f"{P}/scratch", (t, t))
print("demo ready:", P)

# folders: date each one by its newest content, like a real tree that was worked in
for dp, dn, fn in os.walk(P, topdown=False):
    if "/.git" in dp or dp.endswith("/.git") or not (dn or fn):
        continue
    newest = 0
    for name in dn + fn:
        if name == ".git":
            continue
        try:
            newest = max(newest, os.lstat(os.path.join(dp, name)).st_mtime)
        except OSError:
            pass
    if newest:
        os.utime(dp, (newest, newest))
print("folder times set")
