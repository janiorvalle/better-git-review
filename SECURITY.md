# Security

Please report security issues through GitHub's private vulnerability reporting
for this repository. Do not open a public issue containing exploit details,
credentials, private source code, or other sensitive data.

You should receive an initial response within three business days. Security
fixes target the latest supported release.

## Trust Model

`bgr` reads local diffs and may send their contents to the analysis provider
the user selected. Repository provider settings require explicit trust before
they can change the provider, endpoint, or credential environment variable.
CLI providers run in isolated workspaces with their tool access disabled.

Reports are most useful when they identify the input, affected version,
concrete impact, and a minimal reproduction. Scanner output without a credible
impact path is not enough by itself, but uncertain reports are still welcome
through the private channel.
