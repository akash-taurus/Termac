# Assets

Application assets for the Terminal Dashboard (Termac).

| File | Purpose |
|---|---|
| `icon.ico` | Windows application icon — multi-resolution (256/48/32/16 px). Used by the build scripts (rsrc/syso embedding) and the Windows Terminal profile registered with the `w` shortcut. |
| `icon.png` | 512×512 master render, kept for READMEs, store listings, and repo previews. |
| `generate_icon.py` | Reproducible generator for both files (Pillow). Edit it and re-run rather than editing the binary assets by hand. |

## Regenerating the icon

```powershell
python -m pip install Pillow
python assets/generate_icon.py
```

The design uses the dashboard's default **Cyan Cyber** palette (see
`pkg/theme/theme.go`): a terminal window with prompt chevron and cursor on the
deep-obsidian background.

## Embedding into the exe

The Windows build embeds `icon.ico` via a `.syso` resource (see
`Makefile` / `build_windows.ps1`). The generated `.syso` files are build
artifacts and are git-ignored (`*.syso`).
