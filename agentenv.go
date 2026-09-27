package main

import (
	"os"
	"regexp"
	"strings"
)

// Unattended agents run repository code, so they inherit only what a shell,
// git and the runner need. Ralph's own provider keys stay in this process.
var agentEnvNames = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "TMPDIR": true,
	"TERM": true, "COLORTERM": true, "NO_COLOR": true, "LANG": true, "LANGUAGE": true, "TZ": true,
	"XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_STATE_HOME": true, "XDG_CACHE_HOME": true, "XDG_RUNTIME_DIR": true,
	"SSH_AUTH_SOCK": true, "GIT_SSH_COMMAND": true, "GNUPGHOME": true, "GPG_TTY": true,
	"GIT_AUTHOR_NAME": true, "GIT_AUTHOR_EMAIL": true, "GIT_COMMITTER_NAME": true, "GIT_COMMITTER_EMAIL": true,
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true, "http_proxy": true, "https_proxy": true, "no_proxy": true,
	"SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "NODE_EXTRA_CA_CERTS": true,
}

var agentEnvPrefixes = []string{"LC_", "OPENCODE"}

var configEnvRef = regexp.MustCompile(`\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

// runnerEnvNames returns variables the runner's own config resolves through
// {env:NAME}, plus any names listed in RALPH_AGENT_ENV (comma separated).
func runnerEnvNames() []string {
	var names []string
	for _, path := range opencodeConfigPaths() {
		if b, err := os.ReadFile(path); err == nil {
			for _, m := range configEnvRef.FindAllSubmatch(b, -1) {
				names = append(names, string(m[1]))
			}
		}
	}
	for _, name := range strings.Split(os.Getenv("RALPH_AGENT_ENV"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// agentEnv filters environ to the allowlist plus explicitly required names.
func agentEnv(environ, required []string) []string {
	extra := map[string]bool{}
	for _, name := range required {
		extra[name] = true
	}
	var env []string
	for _, kv := range environ {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		allowed := agentEnvNames[name] || extra[name]
		for _, prefix := range agentEnvPrefixes {
			allowed = allowed || strings.HasPrefix(name, prefix)
		}
		if allowed {
			env = append(env, kv)
		}
	}
	return env
}
