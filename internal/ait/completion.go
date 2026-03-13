package ait

import (
	"fmt"
	"strings"
)

// RunCompletion prints a shell completion script for the given shell.
func RunCompletion(shell string) error {
	switch shell {
	case "bash":
		fmt.Print(generateBashCompletion())
		return nil
	case "zsh":
		fmt.Print(generateZshCompletion())
		return nil
	default:
		return &CLIError{
			Code:     "usage",
			Message:  fmt.Sprintf("unsupported shell %q (supported: bash, zsh)", shell),
			ExitCode: 64,
		}
	}
}

func generateBashCompletion() string {
	cmdNames := strings.Join(CommandNames(), " ")

	// Build per-command flag completion cases from the registry.
	var flagCases strings.Builder
	for _, cmd := range commands {
		if len(cmd.Flags) == 0 {
			continue
		}
		flags := strings.Join(cmd.Flags, " ")
		fmt.Fprintf(&flagCases, "        %s)\n", cmd.Name)
		fmt.Fprintf(&flagCases, "            if [[ \"${cur}\" == -* ]]; then\n")
		fmt.Fprintf(&flagCases, "                COMPREPLY=($(compgen -W \"%s\" -- \"${cur}\"))\n", flags)
		fmt.Fprintf(&flagCases, "                return\n")
		fmt.Fprintf(&flagCases, "            fi\n")
		fmt.Fprintf(&flagCases, "            ;;&\n")
	}

	return fmt.Sprintf(`_ait_completions() {
    local cur prev words cword
    # Use _init_completion if available (bash-completion package),
    # otherwise set variables manually for compatibility.
    if declare -F _init_completion >/dev/null 2>&1; then
        _init_completion || return
    else
        COMPREPLY=()
        cur="${COMP_WORDS[COMP_CWORD]}"
        prev="${COMP_WORDS[COMP_CWORD-1]}"
        words=("${COMP_WORDS[@]}")
        cword=${COMP_CWORD}
    fi

    local commands="%s"
    local dep_subcmds="add remove list tree"
    local note_subcmds="add list"
    local completion_subcmds="bash zsh"
    local statuses="open in_progress closed cancelled"
    local types="task epic initiative"
    local priorities="P0 P1 P2 P3 P4"

    # Commands that accept an issue ID as first positional arg
    local id_commands="show update close reopen cancel claim unclaim export"

    if [[ ${cword} -eq 1 ]]; then
        COMPREPLY=($(compgen -W "${commands}" -- "${cur}"))
        return
    fi

    local cmd="${words[1]}"

    # Value completions for flags that take specific values.
    case "${cmd}" in
        list|create|update|ready)
            case "${prev}" in
                --status)   COMPREPLY=($(compgen -W "${statuses}" -- "${cur}")); return ;;
                --type)     COMPREPLY=($(compgen -W "${types}" -- "${cur}")); return ;;
                --priority) COMPREPLY=($(compgen -W "${priorities}" -- "${cur}")); return ;;
                --parent)
                    local ids
                    ids=$(ait list --all 2>/dev/null | grep -o '"id": *"[^"]*"' | sed 's/"id": *"//;s/"//')
                    COMPREPLY=($(compgen -W "${ids}" -- "${cur}"))
                    return
                    ;;
            esac
            ;;
    esac

    # Subcommand and special-case completions.
    case "${cmd}" in
        dep)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=($(compgen -W "${dep_subcmds}" -- "${cur}"))
                return
            fi
            if [[ ${cword} -ge 3 && "${cur}" != -* ]]; then
                local ids
                ids=$(ait list --all 2>/dev/null | grep -o '"id": *"[^"]*"' | sed 's/"id": *"//;s/"//')
                COMPREPLY=($(compgen -W "${ids}" -- "${cur}"))
                return
            fi
            ;;
        note)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=($(compgen -W "${note_subcmds}" -- "${cur}"))
                return
            fi
            if [[ ${cword} -eq 3 && "${cur}" != -* ]]; then
                local ids
                ids=$(ait list --all 2>/dev/null | grep -o '"id": *"[^"]*"' | sed 's/"id": *"//;s/"//')
                COMPREPLY=($(compgen -W "${ids}" -- "${cur}"))
                return
            fi
            ;;
        completion)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=($(compgen -W "${completion_subcmds}" -- "${cur}"))
            fi
            return
            ;;
        # Per-command flag completions (generated from registry).
%s    esac

    # Issue ID completion for commands that take IDs
    for c in ${id_commands}; do
        if [[ "${cmd}" == "${c}" && "${cur}" != -* ]]; then
            local ids
            ids=$(ait list --all 2>/dev/null | grep -o '"id": *"[^"]*"' | sed 's/"id": *"//;s/"//')
            COMPREPLY=($(compgen -W "${ids}" -- "${cur}"))
            return
        fi
    done
}

complete -F _ait_completions ait
`, cmdNames, flagCases.String())
}

func generateZshCompletion() string {
	// Build command descriptions for zsh from registry.
	var cmdDescs strings.Builder
	for _, cmd := range commands {
		fmt.Fprintf(&cmdDescs, "        '%s:%s'\n", cmd.Name, cmd.Summary)
	}

	return fmt.Sprintf(`#compdef ait

_ait() {
    local -a commands
    commands=(
%s    )

    local -a statuses=(open in_progress closed cancelled)
    local -a types=(task epic initiative)
    local -a priorities=(P0 P1 P2 P3 P4)

    _ait_issue_ids() {
        local -a ids
        ids=(${(f)"$(ait list --all 2>/dev/null | grep -o '"id": *"[^"]*"' | sed 's/"id": *"//;s/"//')"})
        compadd -a ids
    }

    if (( CURRENT == 2 )); then
        _describe 'command' commands
        return
    fi

    local cmd="${words[2]}"

    case "${cmd}" in
        dep)
            if (( CURRENT == 3 )); then
                local -a dep_subcmds=(
                    'add:Add a dependency'
                    'remove:Remove a dependency'
                    'list:List dependencies'
                    'tree:Show dependency tree'
                )
                _describe 'subcommand' dep_subcmds
            else
                _ait_issue_ids
            fi
            ;;
        note)
            if (( CURRENT == 3 )); then
                local -a note_subcmds=(
                    'add:Add a note'
                    'list:List notes'
                )
                _describe 'subcommand' note_subcmds
            elif (( CURRENT == 4 )); then
                _ait_issue_ids
            fi
            ;;
        completion)
            if (( CURRENT == 3 )); then
                local -a shells=(bash zsh)
                compadd -a shells
            fi
            ;;
        list)
            _arguments \
                '--all[Include closed and cancelled]' \
                '--long[Full JSON output]' \
                '--human[Human-readable table]' \
                '--tree[Tree view]' \
                '--status[Filter by status]:status:(${statuses})' \
                '--type[Filter by type]:type:(${types})' \
                '--priority[Filter by priority]:priority:(${priorities})' \
                '--parent[Filter by parent]:id:_ait_issue_ids'
            ;;
        create)
            _arguments \
                '--title[Issue title]:title:' \
                '--description[Issue description]:description:' \
                '--type[Issue type]:type:(${types})' \
                '--parent[Parent issue]:id:_ait_issue_ids' \
                '--priority[Priority]:priority:(${priorities})'
            ;;
        update)
            if (( CURRENT == 3 )); then
                _ait_issue_ids
            else
                _arguments \
                    '--title[New title]:title:' \
                    '--description[New description]:description:' \
                    '--status[New status]:status:(${statuses})' \
                    '--priority[New priority]:priority:(${priorities})' \
                    '--parent[New parent]:id:_ait_issue_ids'
            fi
            ;;
        ready)
            _arguments \
                '--long[Full JSON output]' \
                '--type[Filter by type]:type:(${types})'
            ;;
        close)
            if [[ "${words[CURRENT]}" == -* ]]; then
                _arguments '--cascade[Close entire subtree]'
            else
                _ait_issue_ids
            fi
            ;;
        export)
            if [[ "${words[CURRENT]}" == -* ]]; then
                _arguments '--output[Output file]:file:_files'
            else
                _ait_issue_ids
            fi
            ;;
        flush)
            _arguments '--dry-run[Show what would be flushed]'
            ;;
        init)
            _arguments '--prefix[Project prefix]:prefix:'
            ;;
        show|reopen|cancel|unclaim)
            _ait_issue_ids
            ;;
        claim)
            if (( CURRENT == 3 )); then
                _ait_issue_ids
            fi
            ;;
    esac
}

_ait "$@"
`, cmdDescs.String())
}
