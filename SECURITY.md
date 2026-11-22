# Security Policy

## Supported versions

Security fixes are made on the latest released version of soapbridge. Please
upgrade (`brew upgrade soapbridge`, or `go install github.com/harshhh28/soapbridge/cmd/soapbridge@latest`)
before reporting, in case the issue is already fixed.

## Reporting a vulnerability

Please **do not open a public issue** for a security problem.

Report it privately through GitHub: go to the repository's **Security** tab and
choose **Report a vulnerability**
(<https://github.com/harshhh28/soapbridge/security/advisories/new>).

Include what you found, the soapbridge version (`soapbridge --version`), and
steps or a WSDL that reproduces it. You can expect an initial response within
a few days.

## Scope

soapbridge generates and runs gateways that call SOAP services on your behalf.
Things worth reporting include credential handling (the WS-Security
UsernameToken and `X-API-Key` / Basic auth pass-through), injection through
crafted WSDL or request input, and unsafe handling of the generated server's
output paths. Vulnerabilities in the third-party SOAP services you point it at
are out of scope.
