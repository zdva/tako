# Reporting the current folder (OSC 7)

Tako never installs anything on a server. To open the transfer sheet in the
folder you are in, and to open a new tab in the same folder, it listens for the
standard "current working directory" report, OSC 7:

    ESC ] 7 ; file://HOST/PATH BEL

macOS's shells send it already. Most Linux shells do not; add one of these to
the server's shell startup file (the command palette has *Copy the shell hook
that reports the current folder* to put the bash/zsh one on the clipboard):

```sh
# bash (~/.bashrc) and zsh (~/.zshrc)
__tako_osc7() { printf '\033]7;file://%s%s\033\\' "${HOSTNAME:-$(hostname)}" "$PWD"; }
if [ -n "$BASH_VERSION" ]; then PROMPT_COMMAND="__tako_osc7${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
elif [ -n "$ZSH_VERSION" ]; then precmd_functions+=(__tako_osc7); fi
```

```fish
# ~/.config/fish/config.fish
function __tako_osc7 --on-variable PWD
    printf '\033]7;file://%s%s\033\\' (hostname) (string escape --style=url -- $PWD)
end
__tako_osc7
```

Paths with spaces or non-ASCII characters should be percent-encoded in the URL
(zsh and fish helpers do this; the bash line above does not, which is fine for
ordinary paths).

Check it works: open the transfer sheet (`Ctrl+Shift+Alt+T`); the source pane
should show the folder your shell is in. With no report it shows the home
folder.
