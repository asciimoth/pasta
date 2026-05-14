module github.com/asciimoth/pasta/demo

go 1.25.5

require github.com/asciimoth/pasta/pasta v0.0.0

require (
	github.com/asciimoth/configer/configer v0.3.0 // indirect
	github.com/asciimoth/pasta/internal/pastatestcheck v0.0.0 // indirect
)

replace github.com/asciimoth/pasta/pasta => ../pasta

replace github.com/asciimoth/pasta/internal/pastatestcheck => ../internal/pastatestcheck
