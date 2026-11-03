# Re-recording the README previews

The previews are recorded with [VHS](https://github.com/charmbracelet/vhs) in an anonymous demo workspace.

```sh
python3 make_demo.py                       # builds /var/tmp/glint-demo (~1.6 GB of fake build output)
cd /var/tmp/glint-demo && mkdir -p tapes && cp <this folder>/*.tape tapes/ && cd tapes
for t in hero browser dashboard diskmap thumbs; do vhs $t.tape & done; wait

# GIFs are used as-is; stills are the last frame of the MP4s
cp hero.gif browser.gif <repo>/assets/
for v in dashboard diskmap thumbs; do ffmpeg -y -sseof -0.05 -i $v.mp4 -frames:v 1 <repo>/assets/$v.png; done

rm -rf /var/tmp/glint-demo                 # free the space again
```

Needs `vhs` (with `ttyd` and `ffmpeg`), `fallocate`, git and a JetBrainsMono Nerd Font.
`common.tape` sets a fake `HOME` and `--no-owner`, so no real user or path ends up in the images.
