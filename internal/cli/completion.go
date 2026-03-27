package cli

// BashCompletion returns a bash completion script for ai-shim.
func BashCompletion() string {
	return `_ai_shim() {
    local cur prev commands manage_commands
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    commands="version update init manage completion help"
    manage_commands="agents profiles config doctor symlinks dry-run cleanup status exec watch switch-profile"

    case "${prev}" in
        ai-shim)
            COMPREPLY=( $(compgen -W "${commands}" -- "${cur}") )
            return 0
            ;;
        manage)
            COMPREPLY=( $(compgen -W "${manage_commands}" -- "${cur}") )
            return 0
            ;;
        completion)
            COMPREPLY=( $(compgen -W "bash zsh" -- "${cur}") )
            return 0
            ;;
    esac
}
complete -F _ai_shim ai-shim
`
}

// ZshCompletion returns a zsh completion script for ai-shim.
func ZshCompletion() string {
	return `#compdef ai-shim

_ai_shim() {
    local -a commands manage_commands

    commands=(
        'version:Print version'
        'update:Check for and install updates'
        'init:Initialize configuration'
        'manage:Management commands'
        'completion:Generate shell completion'
        'help:Show help'
    )

    manage_commands=(
        'agents:List available agents'
        'profiles:List profiles'
        'config:Show resolved config'
        'doctor:Run diagnostics'
        'symlinks:Manage symlinks'
        'dry-run:Preview container config'
        'cleanup:Remove orphaned resources'
        'status:Show running containers'
        'exec:Execute command in running container'
        'watch:Restart agent on crash with retries'
        'switch-profile:Set the default profile'
    )

    case "${words[2]}" in
        manage)
            _describe -t commands 'manage commands' manage_commands
            ;;
        completion)
            _values 'shell' bash zsh
            ;;
        *)
            _describe -t commands 'ai-shim commands' commands
            ;;
    esac
}

_ai_shim
`
}
