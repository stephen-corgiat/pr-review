// Package shellcomp holds the per-shell completion scripts: a few lines
// each, which call the hidden `pr-review __complete` and do nothing else.
package shellcomp

import "fmt"

// Shells are the supported shells.
var Shells = []string{"bash", "zsh", "fish"}

// Script is shell's completion script, or an error for an unknown shell.
func Script(shell string) (string, error) {
	switch shell {
	case "bash":
		return bash, nil
	case "zsh":
		return zsh, nil
	case "fish":
		return fish, nil
	}
	return "", fmt.Errorf("unknown shell %q (want bash, zsh or fish)", shell)
}

// The directive is the number on the last line of `pr-review __complete`:
// 4 means no file completion, 16 directories only.

const bash = `# bash completion for pr-review: source <(pr-review completion bash)
_pr_review() {
    local out directive
    out=$(pr-review __complete "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null) || return
    directive=${out##*:}
    out=${out%:*}
    COMPREPLY=()
    if [ "$directive" = 16 ]; then
        local IFS=$'\n'
        COMPREPLY=($(compgen -d -- "${COMP_WORDS[COMP_CWORD]}"))
        return
    fi
    local line IFS=$'\n'
    for line in $out; do
        COMPREPLY+=("${line%%$'\t'*}")
    done
    if [ ${#COMPREPLY[@]} -eq 0 ] && [ "$directive" = 0 ]; then
        COMPREPLY=($(compgen -f -- "${COMP_WORDS[COMP_CWORD]}"))
    fi
}
complete -o default -F _pr_review pr-review
`

const zsh = `#compdef pr-review
# zsh completion for pr-review: save as _pr-review in a directory on $fpath
_pr-review() {
    local out directive
    local -a lines cands
    out=$(pr-review __complete "${(@)words[2,CURRENT]}" 2>/dev/null) || return
    lines=("${(@f)out}")
    directive=${lines[-1]#:}
    lines=("${(@)lines[1,-2]}")
    if [[ $directive == 16 ]]; then
        _path_files -/
        return
    fi
    local line name
    for line in $lines; do
        name=${line%%$'\t'*}
        name=${name//:/\\:}
        if [[ $line == *$'\t'* ]]; then
            cands+=("$name:${line#*$'\t'}")
        else
            cands+=("$name")
        fi
    done
    if (( ${#cands} )); then
        _describe -t pr-review 'pr-review' cands
    elif [[ $directive == 0 ]]; then
        _files
    fi
}
if [[ ${funcstack[1]} == _pr-review ]]; then
    _pr-review "$@"
else
    compdef _pr-review pr-review
fi
`

const fish = `# fish completion for pr-review: save as ~/.config/fish/completions/pr-review.fish
function __pr_review_complete
    set -l words (commandline -opc)[2..-1] (commandline -ct)
    set -l out (pr-review __complete $words 2>/dev/null)
    or return
    set -l directive (string replace -r '^:' '' -- $out[-1])
    if test "$directive" = 16
        __fish_complete_directories (commandline -ct)
        return
    end
    for line in $out[1..-2]
        echo $line
    end
end
complete -c pr-review -f -a '(__pr_review_complete)'
`
