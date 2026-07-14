module github.com/asciimoth/pasta/demo/backend

go 1.25.5

require (
	github.com/asciimoth/configer/configer v0.3.1
	github.com/asciimoth/configer/hujson v0.3.1
	github.com/asciimoth/formular v0.3.1
	github.com/asciimoth/gonnect v0.19.0
	github.com/asciimoth/pasta/pasta v0.0.0
	github.com/asciimoth/socksgo v0.2.24
	github.com/tailscale/hujson v0.0.0-20260302212456-ecc657c15afd
)

require (
	github.com/asciimoth/bufpool v0.3.0 // indirect
	github.com/asciimoth/ident v0.2.0 // indirect
	github.com/asciimoth/persist v0.2.0 // indirect
	github.com/asciimoth/putback v0.3.0 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/xtaci/smux v1.5.44 // indirect
	golang.org/x/net v0.54.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
)

replace github.com/asciimoth/pasta/pasta => ../../pasta
