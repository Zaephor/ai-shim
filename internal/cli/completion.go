package cli

// BashCompletion returns a bash completion script for ai-shim.
func BashCompletion() string {
	return `_ai_shim() {
    local cur prev commands manage_commands
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    commands="version update init manage completion help"
    manage_commands="agents profiles config doctor symlinks dry-run backup restore disk-usage cleanup status agent-versions reinstall exec attach stop logs watch switch-profile delete-profile warm"

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
        'backup:Backup a profile to tar.gz'
        'restore:Restore a profile from backup'
        'disk-usage:Show storage usage breakdown'
        'cleanup:Remove orphaned resources'
        'status:Show running containers'
        'agent-versions:Show installed agent versions'
        'reinstall:Force reinstall an agent'
        'exec:Execute command in running container'
        'attach:Reattach to a running container'
        'stop:Stop a running session'
        'logs:Show launch/exit logs or container logs'
        'watch:Restart agent on crash with retries'
        'switch-profile:Set the default profile'
        'delete-profile:Remove a profile and its data'
        'warm:Pre-warm image and caches for an agent'
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
