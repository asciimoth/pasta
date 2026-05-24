module github.com/asciimoth/pasta/pasta

go 1.25.5

replace github.com/asciimoth/pasta/internal/pastatestcheck => ../internal/pastatestcheck

require (
	github.com/asciimoth/badlock v0.1.1
	github.com/wk8/go-ordered-map/v2 v2.1.8
)

require (
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
