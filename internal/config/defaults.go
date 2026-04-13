package config

// DefaultConfigJSON is the config file written by `braid init`.
const DefaultConfigJSON = `{
  "sandbox": "agent",
  "animation": "strip",
  "agent": "claude",
  "env": ["CLAUDE_CODE_OAUTH_TOKEN"],
  "steps": {
    "work": {},
    "review": {},
    "gate": {},
    "iterate": {},
    "ralph": {}
  },
  "retry": {
    "enabled": true,
    "pollIntervalMinutes": 5,
    "maxWaitMinutes": 360
  }
}
`

// DefaultDockerJSON is the docker config written by `braid init`.
const DefaultDockerJSON = `{
  "network": {
    "mode": "restricted",
    "allowedHosts": [
      "api.anthropic.com"
    ]
  }
}
`

// DefaultGitignore is appended to (or created as) .braid/.gitignore.
const DefaultGitignore = `logs/
`
