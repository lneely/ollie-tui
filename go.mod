module ollie-tui

go 1.25.6

require (
	github.com/hymkor/go-multiline-ny v0.22.4
	github.com/mattn/go-runewidth v0.0.19
	github.com/mattn/go-tty v0.0.7
	github.com/nyaosorg/go-readline-ny v1.14.1
	github.com/nyaosorg/go-ttyadapter v0.3.0
	golang.org/x/sys v0.41.0
	ollie v0.0.0-00010101000000-000000000000
)

require (
	github.com/clipperhouse/stringish v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.5.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace ollie => ../ollie

replace anvillm => ../anvillm/main
