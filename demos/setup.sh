# Sourced by each tape in a hidden prelude: `source demos/setup.sh`, from the
# repo root inside its oku environment. It builds fude from the working tree
# and copies the repo, with the same changes, into a throwaway HOME under
# /tmp/fude-demo. A recording never touches your own config, state or files.

repo=$PWD
root=/tmp/fude-demo
go=$(command -v go)
gopls=$(command -v gopls)
modcache=$(go env GOMODCACHE)
gocache=$(go env GOCACHE)
grammars=${XDG_DATA_HOME:-$HOME/.local/share}/fude/grammars

chmod -R u+rwX $root 2>/dev/null
rm -rf $root
mkdir -p $root/bin $root/run $root/.config/fude
go build -o $root/bin/fude ./cmd/fude || return 1
ln -s $go $root/bin/go
ln -s $gopls $root/bin/gopls
git clone -q $repo $root/fude
git -C $repo diff --no-ext-diff --no-color --binary HEAD | git -C $root/fude apply || return 1
cp $repo/demos/config.toml $root/.config/fude/config.toml
# Your built grammars, so the clip shows colours from its first frame.
mkdir -p $root/.local/share/fude
[ -d $grammars ] && ln -s $grammars $root/.local/share/fude/grammars

export HOME=$root
export XDG_CONFIG_HOME=$root/.config
export XDG_STATE_HOME=$root/.local/state
export XDG_DATA_HOME=$root/.local/share
export XDG_RUNTIME_DIR=$root/run
export GOMODCACHE=$modcache GOCACHE=$gocache
export PATH=$root/bin:/usr/bin:/bin:/usr/sbin:/sbin
export TERM=xterm-256color COLORTERM=truecolor
PROMPT='%F{blue}%~%f %F{magenta}❯%f '
cd $root/fude
printf '\033[2J\033[3J\033[H'
