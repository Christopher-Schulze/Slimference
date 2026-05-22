package main

import (
	"fmt"
	"os"
)

// handleCompletionCmd implements `slimference completion bash`. The emitted
// script is sourceable in a bash session and offers context-sensitive
// completion for every top-level subcommand, the most common nested
// subcommands (hook install|remove, proxy install|env|run|..., debug
// paths|last|flight|..., daemon logs, service install|..., config init|show,
// test anthropic|openai), and the recurring period/flag tokens
// (today|week|month|all, --json, --csv, --by-command, --by-parser, --cache,
// --output).
//
// Scope: bash only. zsh/fish are out of scope (T32).
//
// Install with one of:
//
//	slimference completion bash > ~/.bash_completion.d/slimference
//	source <(slimference completion bash)
func handleCompletionCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: slimference completion bash")
		exitFn(1)
		return
	}
	switch args[0] {
	case "bash":
		fmt.Print(bashCompletionScript)
	default:
		fmt.Fprintf(os.Stderr, "unsupported shell: %s (only bash is supported)\n", args[0])
		exitFn(1)
	}
}

// bashCompletionScript is the sourceable bash completion for slimference.
// Keep in lockstep with the top-level dispatch in handleSubcommand and the
// nested dispatchers (handleHookCmd, handleDebugCmd, handleDaemonCmd,
// handleServiceCmd, handleProxyCmd, handleConfigCmd, handleTestCmd).
const bashCompletionScript = `# slimference bash completion
# Source this file or add to ~/.bash_completion.d/slimference .

_slimference() {
    local cur prev words cword
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    cword=$COMP_CWORD

    local top_level="install uninstall enable disable status cert-trust root-arm root-disarm lab config test doctor stats gain savings quality soak compress-preview watch filter rewrite posttool readhook codexhook hook debug daemon start stop restart service codex proxy integrate bypass layer2 completion expand checkpoint trust version"
    local periods="today week month all"
    local period_flags="--json --csv --by-command --by-parser --cache --output"
    local savings_flags="--json --csv --project"
    local quality_flags="--json --url"
    local filter_flags="--stream --"
    local bypass_verbs="on off status"
    local bypass_scoped_flags="--duration= --next-request --next-request="

    if [ "$cword" -eq 1 ]; then
        COMPREPLY=( $(compgen -W "$top_level" -- "$cur") )
        return 0
    fi

    local sub="${COMP_WORDS[1]}"
    case "$sub" in
        install)
            COMPREPLY=( $(compgen -W "--dry-run --json --no-hooks --with-claude --no-autostart --with-keychain --no-keychain --system --help" -- "$cur") )
            ;;
        uninstall)
            COMPREPLY=( $(compgen -W "--dry-run --keep-ca --with-claude --no-keychain --system --json --help" -- "$cur") )
            ;;
        config)
            if [ "$cword" -eq 2 ]; then
                COMPREPLY=( $(compgen -W "init show" -- "$cur") )
            fi
            ;;
        test)
            if [ "$cword" -eq 2 ]; then
                COMPREPLY=( $(compgen -W "anthropic openai" -- "$cur") )
            fi
            ;;
        filter)
            COMPREPLY=( $(compgen -W "$filter_flags" -- "$cur") )
            ;;
        hook)
            if [ "$cword" -eq 2 ]; then
                COMPREPLY=( $(compgen -W "install remove verify status check-upstream" -- "$cur") )
            elif [ "$cword" -eq 3 ]; then
                case "${COMP_WORDS[2]}" in
                    install|remove) COMPREPLY=( $(compgen -W "codex" -- "$cur") ) ;;
                esac
            fi
            ;;
        codexhook)
            if [ "$cword" -eq 2 ]; then
                COMPREPLY=( $(compgen -W "session-start permission-request user-prompt-submit stop" -- "$cur") )
            fi
            ;;
        debug)
            if [ "$cword" -eq 2 ]; then
                COMPREPLY=( $(compgen -W "paths last summary tail replay flight" -- "$cur") )
            elif [ "$cword" -eq 3 ]; then
                case "${COMP_WORDS[2]}" in
                    summary) COMPREPLY=( $(compgen -W "$periods --json" -- "$cur") ) ;;
                    last|tail) COMPREPLY=( $(compgen -W "--json" -- "$cur") ) ;;
                    flight) COMPREPLY=( $(compgen -W "last tail replay export" -- "$cur") ) ;;
                esac
            elif [ "$cword" -eq 4 ]; then
                case "${COMP_WORDS[2]}:${COMP_WORDS[3]}" in
                    flight:last|flight:tail|flight:replay) COMPREPLY=( $(compgen -W "--json" -- "$cur") ) ;;
                    flight:export) COMPREPLY=( $(compgen -W "--csv" -- "$cur") ) ;;
                esac
            fi
            ;;
        daemon)
            if [ "$cword" -eq 2 ]; then
                COMPREPLY=( $(compgen -W "logs" -- "$cur") )
            elif [ "$cword" -ge 3 ] && [ "${COMP_WORDS[2]}" = "logs" ]; then
                COMPREPLY=( $(compgen -W "--path --stream=stdout --stream=stderr --stream=both --lines= --since=" -- "$cur") )
            fi
            ;;
        service)
            if [ "$cword" -eq 2 ]; then
                COMPREPLY=( $(compgen -W "install uninstall status" -- "$cur") )
            fi
            ;;
        proxy)
            if [ "$cword" -eq 2 ]; then
                COMPREPLY=( $(compgen -W "install enable disable status uninstall env run" -- "$cur") )
            elif [ "$cword" -eq 3 ]; then
                case "${COMP_WORDS[2]}" in
                    env|run) COMPREPLY=( $(compgen -W "codex" -- "$cur") ) ;;
                    install|enable|disable|status|uninstall) COMPREPLY=( $(compgen -W "--yes --system --no-launchd --host= --port=" -- "$cur") ) ;;
                esac
            elif [ "$cword" -ge 4 ] && { [ "${COMP_WORDS[2]}" = "env" ] || [ "${COMP_WORDS[2]}" = "run" ]; } && [ "${COMP_WORDS[3]}" = "codex" ]; then
                COMPREPLY=( $(compgen -W "--direct --proxied --proxied-wss --proxied-wss-bridge --transparent-proxied --host= --port= --" -- "$cur") )
            fi
            ;;
        codex)
            if [ "$cword" -eq 2 ]; then
                COMPREPLY=( $(compgen -W "run enable disable status certify recertify desktop launch-desktop" -- "$cur") )
            elif [ "$cword" -ge 3 ]; then
                case "${COMP_WORDS[2]}" in
                    run) COMPREPLY=( $(compgen -W "--direct --transport=auto --transport=http --transport=wss --transport=wss-bridge --host= --port= --" -- "$cur") ) ;;
                    enable) COMPREPLY=( $(compgen -W "--transport=http --transport=wss --host= --port= --dry-run" -- "$cur") ) ;;
                    disable) COMPREPLY=( $(compgen -W "--dry-run" -- "$cur") ) ;;
                    status) COMPREPLY=( $(compgen -W "--json --host= --port=" -- "$cur") ) ;;
                    certify) COMPREPLY=( $(compgen -W "wss --dry-run --operator= --notes= --host= --port=" -- "$cur") ) ;;
                    recertify) COMPREPLY=( $(compgen -W "wss --dry-run --no-write --force --json --operator= --notes= --timeout= --host= --port=" -- "$cur") ) ;;
                    desktop) COMPREPLY=( $(compgen -W "status prove --json --manual --finish --keep-open --duration= --host= --port=" -- "$cur") ) ;;
                    launch-desktop) COMPREPLY=( $(compgen -W "--transport=app-server --transport=proxy --transport=base-url --probe --with-ca-env --host= --port= --app= --env= --insecure-skip-cert-trust-check" -- "$cur") ) ;;
                esac
            fi
            ;;
        enable)
            COMPREPLY=( $(compgen -W "--transport=http --transport=wss --host= --port= --dry-run" -- "$cur") )
            ;;
        disable)
            COMPREPLY=( $(compgen -W "--dry-run" -- "$cur") )
            ;;
        status)
            COMPREPLY=( $(compgen -W "--json --preflight" -- "$cur") )
            ;;
        lab)
            if [ "$cword" -eq 2 ]; then
                COMPREPLY=( $(compgen -W "cert-trust root-arm root-disarm enable disable" -- "$cur") )
            elif [ "$cword" -ge 3 ]; then
                case "${COMP_WORDS[2]}" in
                    root-arm) COMPREPLY=( $(compgen -W "--global-chatgpt-hosts --help" -- "$cur") ) ;;
                    enable|disable) COMPREPLY=( $(compgen -W "--config= --help" -- "$cur") ) ;;
                esac
            fi
            ;;
        completion)
            if [ "$cword" -eq 2 ]; then
                COMPREPLY=( $(compgen -W "bash" -- "$cur") )
            fi
            ;;
        stats)
            if [ "$cword" -eq 2 ]; then
                COMPREPLY=( $(compgen -W "$periods prompt-cache" -- "$cur") )
            elif [ "${COMP_WORDS[2]}" = "prompt-cache" ]; then
                COMPREPLY=( $(compgen -W "$periods --json --csv" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "$periods" -- "$cur") )
            fi
            ;;
        gain)
            COMPREPLY=( $(compgen -W "$periods $period_flags --project" -- "$cur") )
            ;;
        savings)
            COMPREPLY=( $(compgen -W "$periods $savings_flags" -- "$cur") )
            ;;
        quality)
            COMPREPLY=( $(compgen -W "$quality_flags" -- "$cur") )
            ;;
        soak)
            COMPREPLY=( $(compgen -W "$periods --json" -- "$cur") )
            ;;
        compress-preview)
            COMPREPLY=( $(compgen -W "--provider --path --diff --json" -- "$cur") )
            ;;
        watch)
            COMPREPLY=( $(compgen -W "--once --interval --endpoint" -- "$cur") )
            ;;
        bypass)
            if [ "$cword" -eq 2 ]; then
                COMPREPLY=( $(compgen -W "$bypass_verbs" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "$bypass_scoped_flags" -- "$cur") )
            fi
            ;;
        layer2)
            if [ "$cword" -eq 2 ]; then
                COMPREPLY=( $(compgen -W "enable disable status acknowledge ack" -- "$cur") )
            elif [ "$cword" -eq 3 ] && [ "${COMP_WORDS[2]}" = "enable" ]; then
                COMPREPLY=( $(compgen -W "--acknowledge-data-policy" -- "$cur") )
            fi
            ;;
        filter)
            COMPREPLY=( $(compgen -W "--stream --" -- "$cur") )
            ;;
    esac
    return 0
}

complete -F _slimference slimference
`
